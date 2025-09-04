package tests

import (
	"bufio"
	"fmt"
	steosmorphy "github.com/steosofficial/steosmorphy/analyzer"
	"log"
	"os"
	"sync"
	"testing"
	"time"
)

var (
	testAnalyzer     *steosmorphy.MorphAnalyzer
	loadAnalyzerOnce sync.Once
	// Эта переменная нужна, чтобы компилятор не "выкинул" вызовы наших функций
	// как бесполезные. Присваивая результат этой переменной, мы заставляем код выполниться.
	benchmarkResult interface{}
)

// getTestAnalyzer — потокобезопасная функция для получения единственного экземпляра анализатора.
func getTestAnalyzer() *steosmorphy.MorphAnalyzer {
	loadAnalyzerOnce.Do(func() {
		analyzer, err := steosmorphy.LoadMorphAnalyzer()
		if err != nil {
			log.Fatalf("Критическая ошибка: не удалось загрузить словарь для бенчмарка: %v", err)
		}
		testAnalyzer = analyzer
	})
	return testAnalyzer
}

// loadWords загружает указанное количество слов из файла.
// Мы кэшируем результаты, чтобы не перечитывать файл для каждого под-теста.
var wordsCache = make(map[int][]string)

func loadWords(limit int) []string {
	if words, ok := wordsCache[limit]; ok {
		return words
	}

	log.Printf("Загрузка %d слов из test-data.txt для бенчмарка...", limit)
	file, err := os.Open("test-data.txt")
	if err != nil {
		log.Fatalf("Не удалось открыть файл test-data.txt: %v. Убедитесь, что он существует.", err)
	}
	defer file.Close()

	words := make([]string, 0, limit)
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		words = append(words, scanner.Text())
		if len(words) == limit {
			break
		}
	}
	if err := scanner.Err(); err != nil {
		log.Fatalf("Ошибка чтения файла test-data.txt: %v", err)
	}

	if len(words) < limit {
		log.Printf("ВНИМАНИЕ: в файле test-data.txt найдено только %d слов, что меньше запрошенных %d.", len(words), limit)
	}

	wordsCache[limit] = words
	return words
}

// BenchmarkAnalyzeSequential тестирует производительность метода Analyze.
func BenchmarkAnalyzeSequential(b *testing.B) {
	analyzer := getTestAnalyzer()
	wordCounts := []int{10_000}

	for _, count := range wordCounts {
		b.Run(fmt.Sprintf("%d_words", count), func(b *testing.B) {
			words := loadWords(count)
			if len(words) == 0 {
				b.Skip("Нет слов для теста, пропускаем.")
			}

			b.ReportAllocs()
			b.ResetTimer()

			startTime := time.Now()

			for i := 0; i < b.N; i++ {
				for _, word := range words {
					_, benchmarkResult = analyzer.Analyze(word)
				}
			}

			b.StopTimer()

			totalDuration := time.Since(startTime)
			totalWordsProcessed := len(words) * b.N

			if totalWordsProcessed > 0 {
				avgTimePerWord := totalDuration / time.Duration(totalWordsProcessed)
				b.Logf("\n\t--- Кастомная статистика для Analyze (%d слов) ---\n"+
					"\tОбщее время:        %s\n"+
					"\tСреднее на слово:    %s\n"+
					"\tСлов в секунду (RPS): %.0f\n",
					len(words),
					totalDuration.Round(time.Millisecond),
					avgTimePerWord,
					float64(time.Second)/float64(avgTimePerWord),
				)
			}
		})
	}
}

// BenchmarkParseList измеряет производительность пакетной обработки разбора слов.
func BenchmarkParseList(b *testing.B) {
	analyzer := getTestAnalyzer()
	wordCounts := []int{10_000}

	for _, count := range wordCounts {
		b.Run(fmt.Sprintf("%d_words", count), func(b *testing.B) {

			words := loadWords(count)

			b.ReportAllocs()
			b.ResetTimer()

			startTime := time.Now()

			for i := 0; i < b.N; i++ {
				_ = analyzer.ParseList(words)
			}

			b.StopTimer()

			totalDuration := time.Since(startTime)
			totalWordsProcessed := len(words) * b.N

			if totalWordsProcessed > 0 {
				b.Logf("\n\t--- Кастомная статистика для ParseList (%d слов) ---\n"+
					"\tОбщее время:        %s\n",
					len(words),
					totalDuration.Round(time.Millisecond),
				)
			}
		})
	}
}

// BenchmarkInflectList измеряет производительность пакетной обработки поиска словоформ у слов.
func BenchmarkInflectList(b *testing.B) {
	analyzer := getTestAnalyzer()
	wordCounts := []int{10_000} // 1_000_000 слов разом InflectList слишком накладно для ОЗУ

	for _, count := range wordCounts {
		b.Run(fmt.Sprintf("%d_words", count), func(b *testing.B) {

			words := loadWords(count)

			b.ReportAllocs()
			b.ResetTimer()

			startTime := time.Now()

			for i := 0; i < b.N; i++ {
				_ = analyzer.InflectList(words)
			}

			b.StopTimer()

			totalDuration := time.Since(startTime)
			totalWordsProcessed := len(words) * b.N

			if totalWordsProcessed > 0 {
				b.Logf("\n\t--- Кастомная статистика для InflectList (%d слов) ---\n"+
					"\tОбщее время:        %s\n",
					len(words),
					totalDuration.Round(time.Millisecond),
				)
			}
		})
	}
}
