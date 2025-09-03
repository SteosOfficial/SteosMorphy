// Этот файл содержит логику морфологического анализатора.
// Он загружает скомпилированный бинарный словарь (morph_3.dawg) и предоставляет
// API для разбора, склонения и предсказания несловарных слов.
// Ключевая особенность - использование mmap для Zero-Copy загрузки, что минимизирует
// потребление ОЗУ.
package analyzer

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"unsafe"

	"github.com/edsrzf/mmap-go"
)

// --- ПЕРЕМЕННЫЕ ОКРУЖЕНИЯ ---

// EnvDictPath - имя переменной окружения для переопределения пути к словарю.
const EnvDictPath = "STEOSMORPHY_DICT_PATH"

// --- СТРУКТУРЫ ДАННЫХ ---

// MorphInfo - Хранит индексы, указывающие на пулы строк и информацию о парадигме.
type MorphInfo struct {
	LemmaID,
	TagsID,
	ParadigmID uint32
}

// PredictInfo - Хранит полезную информацию в узлах DAWG предсказателя.
type PredictInfo struct {
	Frequency  uint16 // Как часто это правило (суффикс + парадигма) встречалось в словаре.
	ParadigmID uint32 // ID парадигмы, которую нужно использовать для склонения.
	FormIdx    uint32 // Индекс слова-образца в его канонически отсортированной парадигме.
	TagsID     uint32 // ID тегов для этой конкретной формы-образца.
}

// Node - Рекурсивное представление узла Trie/DAWG в оперативной памяти.
// Использует map для дочерних узлов и срезы для полезной нагрузки.
type Node struct {
	Children map[rune]*Node // Дочерние узлы по символу.
	Payload  []any          // Полезная нагрузка. Используем `any` для универсальности (хранит MorphInfo или PredictInfo).
	IsFinal  bool           // Является ли этот узел концом слова/правила.
}

// FlatNode - "Плоское" представление узла для сохранения на диск.
// Вместо указателей используются индексы в глобальных массивах.
type FlatNode struct {
	PayloadIdx, EdgesIdx uint32 // Индексы начала срезов в массивах Payloads и Edges.
	PayloadLen, EdgesLen uint16 // Длины этих срезов.
	IsFinal              bool   // Является ли этот узел концом слова/правила.
}

// FlatEdge - "Плоское" представление ребра графа.
type FlatEdge struct {
	Char   rune   // Символ на ребре.
	NodeID uint32 // ID дочернего узла, на который указывает ребро.
}

// ParadigmInfo - Информация об одной из возможных основ (stems) для парадигмы.
// Нужна для правильного склонения слов с разными основами (бежать/бегу).
type ParadigmInfo struct {
	Stem   string // Сама основа.
	NodeID uint32 // ID узла в "плоском" DAWG, где эта основа заканчивается.
}

// Header - Заголовок бинарного файла morph_3.dawg.
// Это "карта" всего файла, которая позволяет анализатору загружать данные методом Zero-Copy.
type Header struct {
	Magic                 [4]byte // Сигнатура "DAW7" для проверки корректности файла.
	ComplexDataOffset     int64   // Смещение до блока "сложных" данных (в байтах).
	ComplexDataLength     int64   // Длина этого блока (в байтах).
	NodesOffset           int64   // Смещение до массива узлов основного словаря.
	NodesCount            int64   // Количество элементов в этом массиве.
	EdgesOffset           int64   // Смещение до массива ребер основного словаря.
	EdgesCount            int64   // Количество элементов.
	PayloadsOffset        int64   // Смещение до массива payload-ов основного словаря.
	PayloadsCount         int64   // Количество элементов.
	PredictNodesOffset    int64   // Смещение до массива узлов предсказателя.
	PredictNodesCount     int64   // Количество элементов.
	PredictEdgesOffset    int64   // Смещение до массива ребер предсказателя.
	PredictEdgesCount     int64   // Количество элементов.
	PredictPayloadsOffset int64   // Смещение до массива payload-ов предсказателя.
	PredictPayloadsCount  int64   // Количество элементов.
}

// ComplexData - Контейнер для всех данных, которые неэффективно хранить в "сыром" виде.
// Эта часть файла сериализуется с помощью `gob` и полностью загружается в память.
type ComplexData struct {
	LemmaPool         []string                  // Пул всех лемм.
	TagsPool          []string                  // Пул всех наборов тегов.
	Paradigms         map[uint32][]ParadigmInfo // Информация о парадигмах.
	ParadigmToLemmaID map[uint32]uint32         // Карта для быстрого поиска леммы по ID парадигмы.
}

// MorphAnalyzer - основная структура, хранящая все данные и состояние анализатора.
type MorphAnalyzer struct {
	// Данные словаря.
	LemmaPool         []string                  // Пул всех лемм.
	tagsPool          []string                  // Пул всех наборов тегов.
	paradigms         map[uint32][]ParadigmInfo // Информация о парадигмах.
	paradigmToLemmaID map[uint32]uint32         // Карта для быстрого поиска леммы по ID парадигмы.

	// "Сырые" данные, отображенные в память (mmap), но не скопированные в "кучу" Go.
	// Это срезы, указывающие на область памяти, управляемую ОС.
	nodes    []FlatNode  // Узлы основного DAWG.
	edges    []FlatEdge  // Ребра основного DAWG.
	payloads []MorphInfo // Полезная нагрузка основного DAWG.

	predictNodes    []FlatNode    // Узлы DAWG предсказателя.
	predictEdges    []FlatEdge    // Ребра DAWG предсказателя.
	predictPayloads []PredictInfo // Полезная нагрузка DAWG предсказателя.

	// Ссылка на mmap-объект, чтобы он не был собран сборщиком мусора
	// и память оставалась доступной.
	mmapFile mmap.MMap
}

// PredictionCandidate - временная структура для хранения кандидата на предсказание.
// Содержит информацию из словаря и длину совпавшего суффикса.
type PredictionCandidate struct {
	PredictInfo
	SuffixLen int
}

// --- ЛОГИКА АНАЛИЗАТОРА ---

// LoadMorphAnalyzer - конструктор анализатора.
func LoadMorphAnalyzer() (*MorphAnalyzer, error) {
	dictPath := os.Getenv(EnvDictPath)
	if dictPath != "" {
		return loadInternal(dictPath)
	}

	_, currentFilePath, _, ok := runtime.Caller(0)
	if !ok {
		return nil, errors.New("не удалось определить путь к пакету steosmorphy")
	}

	packageDir := filepath.Dir(currentFilePath)
	dictPath = filepath.Join(packageDir, "morph.dawg")

	// Проверяем, существует ли объединенный файл.
	// Если нет, ищем части и объединяем их.
	if _, err := os.Stat(dictPath); os.IsNotExist(err) {
		fmt.Printf("Объединенный файл словаря '%s' не найден. Ищем части для объединения.\n", dictPath)

		// Получаем директорию, где должен находиться файл (или его части)
		dirToSearchParts := filepath.Dir(dictPath)
		if dirToSearchParts == "" { // Если dictPath - просто имя файла, то текущая директория
			dirToSearchParts = "."
		}

		// Вызываем функцию объединения.
		// Имя префикса частей: "morph_"
		// Имя целевого объединенного файла: "morph.dawg" (или что было в dictPath)
		err = mergeFilesWithPrefix(dirToSearchParts, "morph_", dictPath)
		if err != nil {
			// Если произошла ошибка при объединении, проверяем, была ли это ошибка "файлы не найдены".
			// Если да, то, возможно, словарь просто отсутствует.
			if strings.Contains(err.Error(), "не найдено файлов с префиксом") {
				return nil, fmt.Errorf(
					"словарь или его части не найдены по вычисленному пути '%s'. "+
						"Убедитесь, что библиотека установлена корректно и файлы 'morph_aa', 'morph_ab', ... присутствуют. "+
						"Либо установите переменную окружения %s",
					dictPath, EnvDictPath,
				)
			}
			return nil, fmt.Errorf("ошибка при объединении частей словаря: %w", err)
		}
		fmt.Printf("Части словаря успешно объединены в '%s'.\n", dictPath)
	}

	// 4. После того как убедились, что объединенный файл существует (или был создан),
	// передаем его путь в loadInternal.

	if _, err := os.Stat(dictPath); os.IsNotExist(err) {
		return nil, fmt.Errorf(
			"словарь не найден по вычисленному пути '%s'. "+
				"Убедитесь, что библиотека установлена корректно и файл 'morph.dawg' присутствует. "+
				"Либо установите переменную окружения %s",
			dictPath, EnvDictPath,
		)
	}

	return loadInternal(dictPath)
}

// loadInternal Загружает бинарный словарь, читает его заголовок, декодирует "сложную" часть
// и создает "виртуальные" срезы для "сырых" данных.
func loadInternal(filepath string) (*MorphAnalyzer, error) {
	// 1. Открываем файл.
	file, err := os.Open(filepath)
	if err != nil {
		return nil, fmt.Errorf("ошибка открытия файла: %w", err)
	}
	defer file.Close()

	// 2. Отображаем весь файл в виртуальное адресное пространство процесса.
	// Это самая важная операция: файл не копируется в ОЗУ, ОС сама подгружает
	// нужные страницы по мере обращения к ним.
	mmapFile, err := mmap.Map(file, mmap.RDONLY, 0)
	if err != nil {
		return nil, fmt.Errorf("ошибка mmap.Map: %w", err)
	}

	// 3. Читаем заголовок (карту файла) прямо из mmap-среза.
	var header Header
	headerSize := int(unsafe.Sizeof(header))
	if len(mmapFile) < headerSize {
		_ = mmapFile.Unmap()
		return nil, fmt.Errorf("файл слишком мал для заголовка")
	}
	if err := binary.Read(bytes.NewReader(mmapFile[:headerSize]), binary.LittleEndian, &header); err != nil {
		_ = mmapFile.Unmap()
		return nil, fmt.Errorf("ошибка чтения заголовка: %w", err)
	}
	if string(header.Magic[:]) != "DAW7" {
		_ = mmapFile.Unmap()
		return nil, fmt.Errorf("неверная сигнатура файла")
	}

	// 4. Декодируем "сложный" блок (строки, карты) с помощью gob.
	complexStart := header.ComplexDataOffset
	complexEnd := complexStart + header.ComplexDataLength
	compressedBlock := mmapFile[complexStart:complexEnd]

	// 4.1. Распаковываем блок в памяти
	gzipReader, err := gzip.NewReader(bytes.NewReader(compressedBlock))
	if err != nil {
		_ = mmapFile.Unmap()
		return nil, fmt.Errorf("ошибка создания gzip.Reader: %w", err)
	}

	decompressedBytes, err := io.ReadAll(gzipReader)
	if err != nil {
		_ = mmapFile.Unmap()
		return nil, fmt.Errorf("ошибка распаковки данных: %w", err)
	}
	if err := gzipReader.Close(); err != nil {
		_ = mmapFile.Unmap()
		return nil, fmt.Errorf("ошибка закрытия gzip.Reader: %w", err)
	}

	// 4.2 Декодируем РАСПАКОВАННЫЕ байты с помощью gob
	var complexData ComplexData
	if err := gob.NewDecoder(bytes.NewReader(decompressedBytes)).Decode(&complexData); err != nil {
		_ = mmapFile.Unmap()
		return nil, fmt.Errorf("ошибка gob-декодирования: %w", err)
	}

	// 5. Создаем "виртуальные" срезы, используя `bytesToSlice`.
	// Эти срезы не владеют данными, а лишь указывают на нужные участки mmap-файла.
	nodes := bytesToSlice[FlatNode](mmapFile[header.NodesOffset : header.NodesOffset+header.NodesCount*int64(unsafe.Sizeof(FlatNode{}))])
	edges := bytesToSlice[FlatEdge](mmapFile[header.EdgesOffset : header.EdgesOffset+header.EdgesCount*int64(unsafe.Sizeof(FlatEdge{}))])
	payloads := bytesToSlice[MorphInfo](mmapFile[header.PayloadsOffset : header.PayloadsOffset+header.PayloadsCount*int64(unsafe.Sizeof(MorphInfo{}))])
	predictNodes := bytesToSlice[FlatNode](mmapFile[header.PredictNodesOffset : header.PredictNodesOffset+header.PredictNodesCount*int64(unsafe.Sizeof(FlatNode{}))])
	predictEdges := bytesToSlice[FlatEdge](mmapFile[header.PredictEdgesOffset : header.PredictEdgesOffset+header.PredictEdgesCount*int64(unsafe.Sizeof(FlatEdge{}))])
	predictPayloads := bytesToSlice[PredictInfo](mmapFile[header.PredictPayloadsOffset : header.PredictPayloadsOffset+header.PredictPayloadsCount*int64(unsafe.Sizeof(PredictInfo{}))])

	// 6. Инициализируем и возвращаем готовый к работе анализатор.
	analyzer := &MorphAnalyzer{
		LemmaPool:         complexData.LemmaPool,
		tagsPool:          complexData.TagsPool,
		paradigms:         complexData.Paradigms,
		paradigmToLemmaID: complexData.ParadigmToLemmaID,
		nodes:             nodes,
		edges:             edges,
		payloads:          payloads,
		predictNodes:      predictNodes,
		predictEdges:      predictEdges,
		predictPayloads:   predictPayloads,
		mmapFile:          mmapFile,
	}

	return analyzer, nil
}

// mergeFilesWithPrefix объединяет файлы с заданным префиксом в один большой файл.
// sourceDir - директория, где находятся части.
// prefix - префикс имен файлов частей (например, "morph_").
// outputPath - путь к файлу, куда будут записаны объединенные данные.
func mergeFilesWithPrefix(sourceDir, prefix, outputPath string) error {
	// 1. Найти все файлы, начинающиеся с префикса в указанной директории.
	var partFiles []string
	err := filepath.WalkDir(sourceDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() && filepath.Base(path) != outputPath && filepath.HasPrefix(filepath.Base(path), prefix) {
			partFiles = append(partFiles, path)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("ошибка при поиске файлов: %w", err)
	}

	if len(partFiles) == 0 {
		return fmt.Errorf("не найдено файлов с префиксом '%s' в директории '%s'", prefix, sourceDir)
	}

	// 2. Сортировать файлы по имени, чтобы обеспечить правильный порядок.
	// `split` по умолчанию создает файлы с суффиксами `aa`, `ab`, `ac` и т.д.,
	// что обеспечивает правильный лексикографический порядок.
	sort.Strings(partFiles)

	fmt.Printf("Будут объединены следующие части (в порядке):\n")
	for _, part := range partFiles {
		fmt.Printf("- %s\n", filepath.Base(part))
	}

	// 3. Создать или перезаписать выходной файл.
	outFile, err := os.Create(outputPath)
	if err != nil {
		return fmt.Errorf("ошибка создания выходного файла %s: %w", outputPath, err)
	}
	defer outFile.Close() // Закрыть выходной файл в конце

	// 4. Скопировать содержимое каждой части в выходной файл.
	for _, partPath := range partFiles {
		inFile, err := os.Open(partPath)
		if err != nil {
			return fmt.Errorf("ошибка открытия части файла %s: %w", partPath, err)
		}

		_, err = io.Copy(outFile, inFile)
		inFile.Close() // Важно закрыть каждую часть после копирования
		if err != nil {
			return fmt.Errorf("ошибка копирования данных из %s в %s: %w", partPath, outputPath, err)
		}
	}

	fmt.Printf("Все части успешно объединены в файл: %s\n", outputPath)
	return nil
}

// bytesToSlice - "небезопасная" функция, которая создает заголовок среза,
// указывающий на область байт, без копирования самих данных.
func bytesToSlice[T any](b []byte) []T {
	if len(b) == 0 {
		return nil
	}
	var t T
	size := int(unsafe.Sizeof(t))
	header := reflect.SliceHeader{Data: uintptr(unsafe.Pointer(&b[0])), Len: len(b) / size, Cap: len(b) / size}
	return *(*[]T)(unsafe.Pointer(&header))
}

// Analyze - главный публичный метод. Принимает слово и возвращает полный его разбор.
// Работает для словарных и несловарных слов
func (a *MorphAnalyzer) Analyze(word string) ([]*Parsed, []*Parsed) {
	// Сначала пытаемся найти слово в словаре.
	parses := a.Parse(word)
	if len(parses) > 0 {
		// Если нашли, то и все его формы тоже есть в словаре.
		forms := a.Inflect(word)
		return parses, forms
	}
	// Если слово не найдено, пытаемся его предсказать.
	predictedParses := a.ParsePredicted(word)
	if predictedParses == nil {
		// Если и предсказать не удалось, возвращаем nil.
		return nil, nil
	}
	// Если предсказание удалось, генерируем для него все словоформы.
	predictedForms := a.Predict(word, predictedParses[0].Lemma)
	return predictedParses, predictedForms
}

// Inflect генерирует все словоформы для словарного слова.
func (a *MorphAnalyzer) Inflect(word string) []*Parsed {
	// Находим все возможные разборы для введенного слова.
	initialParses := a.Parse(word)
	if len(initialParses) == 0 {
		return nil
	}

	// Собираем уникальные ID парадигм и их леммы.
	paradigmsToProcess := make(map[uint32]string)

	// Для этого нам нужно снова найти payload'ы, чтобы связать разбор с ID парадигмы.
	lowerWord := strings.ToLower(word)
	currentNodeIndex := uint32(0)
	pathFound := true
	for _, char := range lowerWord {
		childNodeIndex, found := a.findChildGeneral(currentNodeIndex, char, a.nodes, a.edges)
		if !found {
			pathFound = false
			break
		}
		currentNodeIndex = childNodeIndex
	}

	if pathFound {
		node := a.nodes[currentNodeIndex]
		payloadStart, payloadEnd := node.PayloadIdx, node.PayloadIdx+uint32(node.PayloadLen)
		for _, info := range a.payloads[payloadStart:payloadEnd] {
			paradigmsToProcess[info.ParadigmID] = a.LemmaPool[info.LemmaID]
		}
	}

	// Генерируем все формы для каждой найденной уникальной парадигмы.
	finalResults := make(map[string]*Parsed) // Используем карту для уникальности результатов.

	for pID, lemma := range paradigmsToProcess {
		// Получаем ВСЕ основы (stems) для данной парадигмы.
		paradigmInfoSlice, ok := a.paradigms[pID]
		if !ok {
			continue
		}

		// Для КАЖДОЙ основы запускаем генерацию.
		for _, pInfo := range paradigmInfoSlice {
			generatedForms := make(map[string]uint32)
			a.dfsGenerate(pInfo.NodeID, []rune(pInfo.Stem), pID, generatedForms)

			for form, tagsID := range generatedForms {
				// Добавляем в итоговую карту.
				if _, exists := finalResults[form]; !exists {
					finalResults[form] = newParsed(form, lemma, a.tagsPool[tagsID])
				}
			}
		}
	}

	if len(finalResults) == 0 {
		return nil
	}

	// Преобразуем карту в отсортированный срез.
	finalList := make([]*Parsed, 0, len(finalResults))
	for _, p := range finalResults {
		finalList = append(finalList, p)
	}

	sort.Slice(finalList, func(i, j int) bool {
		return finalList[i].Word < finalList[j].Word
	})

	return finalList
}

// Parse ищет слово в основном словаре (DAWG).
func (a *MorphAnalyzer) Parse(word string) []*Parsed {
	lowerWord := strings.ToLower(word)
	currentNodeIndex := uint32(0)

	// Идем по графу символ за символом.
	for _, char := range lowerWord {
		childNodeIndex, found := a.findChildGeneral(currentNodeIndex, char, a.nodes, a.edges)
		if !found {
			return nil // Если пути нет, слова в словаре нет.
		}
		currentNodeIndex = childNodeIndex
	}

	node := a.nodes[currentNodeIndex]
	if !node.IsFinal {
		return nil // Дошли до конца слова, но узел не является финальным.
	}

	// Если узел финальный, собираем все варианты разбора, используя его payload.
	var results []*Parsed
	payloadStart, payloadEnd := node.PayloadIdx, node.PayloadIdx+uint32(node.PayloadLen)
	for _, info := range a.payloads[payloadStart:payloadEnd] {
		results = append(results, newParsed(word, a.LemmaPool[info.LemmaID], a.tagsPool[info.TagsID]))
	}
	return results
}

// ParsePredicted пытается предсказать разбор для несловарного слова.
func (a *MorphAnalyzer) ParsePredicted(word string) []*Parsed {
	lowerWord := strings.ToLower(word)
	best := a.findBestPrediction(lowerWord)
	if best == nil {
		return nil
	}

	var predictedLemma string

	// Получаем все формы и лемму для парадигмы-образца.
	allFormsOfTemplate := a.getFormsByParadigmID(best.ParadigmID)
	lemmaID, ok := a.paradigmToLemmaID[best.ParadigmID]

	// Проверяем, что все данные на месте.
	if !ok || len(allFormsOfTemplate) == 0 || int(best.FormIdx) >= len(allFormsOfTemplate) {
		// Fallback: если что-то пошло не так, лемма - само слово.
		predictedLemma = lowerWord
	} else {
		// Логика "пропорциональной замены" для определения леммы.
		wordOfTemplate := allFormsOfTemplate[int(best.FormIdx)]
		lemmaOfTemplate := a.LemmaPool[lemmaID]

		if len([]rune(wordOfTemplate)) < best.SuffixLen {
			// Fallback: слово-образец короче суффикса.
			predictedLemma = lowerWord
		} else {
			commonSuffix := string([]rune(lowerWord)[len([]rune(lowerWord))-best.SuffixLen:])
			if !strings.HasSuffix(wordOfTemplate, commonSuffix) {
				// Fallback: аналогия неполная.
				predictedLemma = lowerWord
			} else {
				// Все проверки пройдены, вычисления безопасны.
				oovPrefix := strings.TrimSuffix(lowerWord, commonSuffix)
				templateWordPrefix := strings.TrimSuffix(wordOfTemplate, commonSuffix)
				if !strings.HasPrefix(lemmaOfTemplate, templateWordPrefix) {
					// Сложный случай (супплетивизм).
					predictedLemma = lowerWord
				} else {
					templateLemmaEnding := strings.TrimPrefix(lemmaOfTemplate, templateWordPrefix)
					predictedLemma = oovPrefix + templateLemmaEnding
				}
			}
		}
	}

	// Теги берем напрямую из найденного правила предсказания.
	tags := a.tagsPool[best.TagsID]
	return []*Parsed{newParsed(word, predictedLemma, tags)}
}

// Predict генерирует все словоформы для несловарного слова.
func (a *MorphAnalyzer) Predict(word string, lemma string) []*Parsed {
	lowerWord := strings.ToLower(word)
	best := a.findBestPrediction(lowerWord)
	if best == nil {
		return nil
	}

	// Получаем слово-образец для вычисления префиксов.
	allFormsInParadigm := a.getFormsByParadigmID(best.ParadigmID)
	if len(allFormsInParadigm) == 0 || int(best.FormIdx) >= len(allFormsInParadigm) {
		return nil
	}
	wordOfTemplate := allFormsInParadigm[int(best.FormIdx)]

	// Проверяем корректность аналогии.
	if len([]rune(wordOfTemplate)) < best.SuffixLen {
		return nil
	}
	commonSuffix := string([]rune(lowerWord)[len([]rune(lowerWord))-best.SuffixLen:])
	if !strings.HasSuffix(wordOfTemplate, commonSuffix) {
		return nil
	}

	// Вычисляем префиксы.
	inputPrefix := strings.TrimSuffix(lowerWord, commonSuffix)
	dictPrefix := strings.TrimSuffix(wordOfTemplate, commonSuffix)

	// Получаем все формы и теги из парадигмы-образца.
	formsAndTags := make(map[string]uint32)
	paradigmInfoSlice, _ := a.paradigms[best.ParadigmID]
	for _, pInfo := range paradigmInfoSlice {
		a.dfsGenerate(pInfo.NodeID, []rune(pInfo.Stem), best.ParadigmID, formsAndTags)
	}

	// Генерируем новые формы, заменяя префикс.
	results := make([]*Parsed, 0, len(formsAndTags))
	for dictForm, tagsID := range formsAndTags {
		if strings.HasPrefix(dictForm, dictPrefix) {
			ending := strings.TrimPrefix(dictForm, dictPrefix)
			newForm := inputPrefix + ending
			results = append(results, newParsed(newForm, lemma, a.tagsPool[tagsID]))
		}
	}

	if len(results) == 0 {
		return nil
	}
	sort.Slice(results, func(i, j int) bool {
		return results[i].Word < results[j].Word
	})
	return results
}

// findBestPrediction ищет лучшее правило предсказания для слова.
// Пробует суффиксы длиной от 5 до 1, ищет их в DAWG предсказателя.
// Среди всех найденных правил выбирает то, у которого самый длинный суффикс,
// а при равенстве длин - самая высокая частота.
func (a *MorphAnalyzer) findBestPrediction(word string) *PredictionCandidate {
	runes := []rune(word)
	var candidates []PredictionCandidate

	// Итерируемся по возможным длинам суффиксов, от самой длинной (5) к самой короткой (1).
	// Это позволяет нам рано прекратить поиск, если будет найдено более специфичное правило.
	for suffixLen := 5; suffixLen >= 1; suffixLen-- {
		// Пропускаем, если слово короче, чем текущая длина суффикса.
		if suffixLen > len(runes) {
			continue
		}

		// Вырезаем суффикс нужной длины и ищем его в DAWG предсказателя.
		suffix := string(runes[len(runes)-suffixLen:])
		currentNodeIndex, foundSuffix := uint32(0), true

		// Обходим DAWG предсказателя.
		for _, char := range suffix {
			childNodeIndex, ok := a.findChildGeneral(currentNodeIndex, char, a.predictNodes, a.predictEdges)
			if !ok {
				foundSuffix = false
				break
			}
			currentNodeIndex = childNodeIndex
		}

		if !foundSuffix || !a.predictNodes[currentNodeIndex].IsFinal {
			continue
		}

		// Если суффикс найден и узел является финальным (т.е. это конец правила),
		// собираем все правила (payloads) из этого узла
		// и добавляем их в наш список кандидатов.
		payloadStart, payloadEnd := a.predictNodes[currentNodeIndex].PayloadIdx,
			a.predictNodes[currentNodeIndex].PayloadIdx+uint32(a.predictNodes[currentNodeIndex].PayloadLen)
		for _, p := range a.predictPayloads[payloadStart:payloadEnd] {
			candidates = append(candidates, PredictionCandidate{PredictInfo: p, SuffixLen: suffixLen})
		}
	}
	// Если ни одного кандидата не найдено, возвращаем nil.
	if len(candidates) == 0 {
		return nil
	}

	// Сортируем всех найденных кандидатов по нашим правилам приоритета
	sort.Slice(candidates, func(i, j int) bool {
		// Сначала сравниваем по длине суффикса (чем больше, тем лучше).
		if candidates[i].SuffixLen != candidates[j].SuffixLen {
			return candidates[i].SuffixLen > candidates[j].SuffixLen
		}
		// Если длины равны, сравниваем по частоте (чем больше, тем лучше).
		return candidates[i].Frequency > candidates[j].Frequency
	})

	// Возвращаем самого лучшего кандидата, который оказался на первом месте после сортировки.
	return &candidates[0]
}

// getFormsByParadigmID возвращает канонически отсортированный срез всех словоформ для данной парадигмы.
// Сортировка важна для того, чтобы FormIdx из предсказателя всегда указывал на одно и то же слово.
func (a *MorphAnalyzer) getFormsByParadigmID(pID uint32) []string {
	// Находим информацию о парадигме, включая все ее возможные основы (stems).
	paradigmInfoSlice, ok := a.paradigms[pID]
	if !ok {
		return nil
	}

	// Используем `dfsGenerate` для сбора всех форм и их тегов в карту resultsMap.
	// resultsMap используется как set, чтобы автоматически избавиться от дубликатов,
	// которые могли бы возникнуть из-за разных основ (stems).
	resultsMap := make(map[string]uint32)
	for _, pInfo := range paradigmInfoSlice {
		a.dfsGenerate(pInfo.NodeID, []rune(pInfo.Stem), pID, resultsMap)
	}

	if len(resultsMap) == 0 {
		return nil
	}

	// Преобразуем ключи карты (уникальные словоформы) в срез.
	forms := make([]string, 0, len(resultsMap))
	for form := range resultsMap {
		forms = append(forms, form)
	}

	// Сортируем срез по алфавиту. Это критически важный шаг для стабильности определения по `FormIdx`.
	sort.Strings(forms)
	return forms
}

// findChildGeneral - универсальная функция поиска дочернего узла по символу.
// Работает с "плоскими" представлениями узлов и ребер.
// Использует бинарный поиск, так как ребра для каждого узла отсортированы.
func (a *MorphAnalyzer) findChildGeneral(nodeIndex uint32, char rune, nodes []FlatNode, edges []FlatEdge) (uint32, bool) {
	// Получаем информацию о текущем узле из глобального массива узлов.
	// Быстрая проверка: если у узла нет исходящих ребер, то и перехода быть не может.
	// Это очень частый случай для листовых узлов, поэтому проверка важна для производительности.
	node := nodes[nodeIndex]
	if node.EdgesLen == 0 {
		return 0, false
	}

	// Определяем "окно" ребер, относящихся ТОЛЬКО к нашему текущему узлу.
	// Вся магия "плоского" представления в том, что ребра для одного узла лежат
	// в глобальном массиве `edges` непрерывным блоком. Мы знаем, где он начинается
	// (EdgesIdx) и какой он длины (EdgesLen).
	edgesStart, edgesEnd := node.EdgesIdx, node.EdgesIdx+uint32(node.EdgesLen)
	searchSlice := edges[edgesStart:edgesEnd]

	// Ключевая оптимизация: используем БИНАРНЫЙ ПОИСК вместо линейного.
	// `sort.Search` возвращает индекс, куда можно было бы вставить `char`, чтобы сохранить порядок.
	// Если на этом месте действительно находится `char`,
	// значит, возвращаем ID узла, на который указывает это ребро.
	i := sort.Search(len(searchSlice), func(i int) bool { return searchSlice[i].Char >= char })
	if i < len(searchSlice) && searchSlice[i].Char == char {
		return searchSlice[i].NodeID, true
	}

	// Если бинарный поиск не нашел точного совпадения, значит, такого ребра нет.
	return 0, false
}

// dfsGenerate рекурсивно обходит DAWG, начиная с узла `nodeIndex`,
// и собирает все возможные словоформы, добавляя к ним префикс.
// Использует поиск в глубину (Depth-First Search).
func (a *MorphAnalyzer) dfsGenerate(nodeIndex uint32, prefix []rune, targetID uint32, results map[string]uint32) {
	// Создаем буфер для накапливания суффикса текущей формы.
	suffixPart := make([]rune, 0)

	// Объявляем рекурсивную функцию `findWord`, чтобы она могла ссылаться сама на себя.
	var findWord func(uint32, []rune)

	findWord = func(currNodeIdx uint32, currentSuffix []rune) {
		// Получаем текущий узел.
		currNode := a.nodes[currNodeIdx]
		//  Проверяем, является ли узел финальным для какого-либо слова.
		if currNode.IsFinal {
			// Если да, то извлекаем его полезную информацию (payload).
			payloadStart, payloadEnd := currNode.PayloadIdx, currNode.PayloadIdx+uint32(currNode.PayloadLen)
			for _, info := range a.payloads[payloadStart:payloadEnd] {
				// Проверяем, относится ли найденная информация к нашей целевой парадигме.
				if info.ParadigmID == targetID {
					// Если да, собираем полную форму (основа + найденный суффикс)
					// и записываем ее в карту результатов вместе с ID ее тегов.
					form := string(append(prefix, currentSuffix...))
					results[form] = info.TagsID
				}
			}
		}

		// Рекурсивно переходим ко всем дочерним узлам.
		edgesStart, edgesEnd := currNode.EdgesIdx, currNode.EdgesIdx+uint32(currNode.EdgesLen)
		for _, edge := range a.edges[edgesStart:edgesEnd] {
			// При переходе к дочернему узлу мы добавляем символ с ребра к нашему суффиксу.
			findWord(edge.NodeID, append(currentSuffix, edge.Char))
		}
	}

	// Запускаем рекурсивный поиск с начального узла и пустым суффиксом.
	findWord(nodeIndex, suffixPart)
}

// ParseList анализирует срез слов в конкурентном режиме, используя пул воркеров.
func (a *MorphAnalyzer) ParseList(words []string) []*Parsed {
	const chunkSize = 1000
	numWorkers := runtime.NumCPU()

	// Канал для отправки "пакетов" (чанков) в воркеры.
	chunksCh := make(chan []string, numWorkers)
	// Канал для сбора результатов от воркеров.
	resultCh := make(chan []*Parsed, numWorkers)

	var wg sync.WaitGroup

	// Запускаем воркеры
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for chunk := range chunksCh {
				parsedChunk := make([]*Parsed, 0, len(chunk))
				for _, word := range chunk {
					parses, _ := a.Analyze(word)
					if parses != nil {
						parsedChunk = append(parsedChunk, parses...)
					}
				}
				resultCh <- parsedChunk
			}
		}()
	}

	// Запускаем диспетчера, который нарезает `words` на чанки и отправляет их в `chunksCh`.
	go func() {
		for i := 0; i < len(words); i += chunkSize {
			end := i + chunkSize
			if end > len(words) {
				end = len(words)
			}
			chunksCh <- words[i:end]
		}
		close(chunksCh) // Закрываем канал, чтобы воркеры завершили работу.
	}()

	// Запускаем "сборщика", который дождется всех воркеров и закроет канал результатов.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Собираем все результаты в один большой срез.
	// Предварительно выделяем память, чтобы избежать лишних аллокаций.
	allParsed := make([]*Parsed, 0, len(words))
	for result := range resultCh {
		allParsed = append(allParsed, result...)
	}

	// Финальная сортировка для консистентного результата.
	sort.Slice(allParsed, func(i, j int) bool {
		return allParsed[i].Word < allParsed[j].Word
	})

	return allParsed
}

// InflectList анализирует срез слов, возвращает срез всех словоформ.
func (a *MorphAnalyzer) InflectList(words []string) []*Parsed {
	const chunkSize = 1000 // Размер одного "пакета" для обработки воркером.
	numWorkers := runtime.NumCPU()

	// Канал для отправки "пакетов" (чанков) в воркеры.
	chunksCh := make(chan []string, numWorkers)
	// Канал для сбора результатов от воркеров.
	resultCh := make(chan []*Parsed, numWorkers)

	var wg sync.WaitGroup

	// Запускаем воркеры
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go func() {
			defer wg.Done()
			for chunk := range chunksCh {
				parsedChunk := make([]*Parsed, 0, len(chunk))
				for _, word := range chunk {
					_, forms := a.Analyze(word)
					if forms != nil {
						parsedChunk = append(parsedChunk, forms...)
					}
				}
				resultCh <- parsedChunk
			}
		}()
	}

	// Запускаем диспетчера, который нарезает `words` на чанки и отправляет их в `chunksCh`.
	go func() {
		for i := 0; i < len(words); i += chunkSize {
			end := i + chunkSize
			if end > len(words) {
				end = len(words)
			}
			chunksCh <- words[i:end]
		}
		close(chunksCh) // Закрываем канал, чтобы воркеры завершили работу.
	}()

	// Запускаем "сборщика", который дождется всех воркеров и закроет канал результатов.
	go func() {
		wg.Wait()
		close(resultCh)
	}()

	// Собираем все результаты в один большой срез.
	// Предварительно выделяем память, чтобы избежать лишних аллокаций.
	allParsed := make([]*Parsed, 0, len(words))
	for result := range resultCh {
		allParsed = append(allParsed, result...)
	}

	// Финальная сортировка для консистентного результата.
	sort.Slice(allParsed, func(i, j int) bool {
		return allParsed[i].Word < allParsed[j].Word
	})

	return allParsed
}
