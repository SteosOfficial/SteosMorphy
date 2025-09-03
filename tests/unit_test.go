// unit_test.go
package tests

import (
	"fmt"
	steosmorphy "github.com/steosofficial/steosmorphy/analyzer"
	"log"
	"os"
	"sort"
	"testing"
)

var analyzer *steosmorphy.MorphAnalyzer

// TestMain - это специальная функция, которая запускается один раз перед всеми тестами в пакете.
func TestMain(m *testing.M) {
	var err error
	analyzer, err = steosmorphy.LoadMorphAnalyzer()
	if err != nil {
		log.Fatalf("Не удалось загрузить анализатор для тестов: %v", err)
	}

	os.Exit(m.Run())
}

// --- ТЕСТЫ ДЛЯ СЛОВАРНЫХ СЛОВ ---

func TestAnalyze_DictionaryWords(t *testing.T) {
	testCases := []struct {
		name          string
		word          string
		expectedLemma string
		expectedPOS   string
		expectedCase  string
		expectedForms []string
	}{
		// --- СУЩЕСТВИТЕЛЬНЫЕ ---
		{
			name:          "Простое существительное (мама)",
			word:          "мама",
			expectedLemma: "мама",
			expectedPOS:   "Существительное",
			expectedCase:  "Именительный",
			expectedForms: []string{"мама", "маме", "мамой", "мамою", "маму", "мамы"},
		},
		{
			name:          "Существительное не в начальной форме (коту)",
			word:          "коту",
			expectedLemma: "кот",
			expectedPOS:   "Существительное",
			expectedCase:  "Дательный",
			expectedForms: []string{"кот", "кота", "коте", "котом", "коту", "коты", "котам", "котами", "котов"},
		},
		{
			name:          "Существительное с супплетивной формой (человек)",
			word:          "человек",
			expectedLemma: "человек",
			expectedPOS:   "Существительное",
			expectedCase:  "Именительный",
			expectedForms: []string{"человек", "человека", "человеку", "человеком", "человеке", "люди", "людей", "людям", "людьми", "людях"},
		},

		// --- ПРИЛАГАТЕЛЬНЫЕ и СУППЛЕТИВИЗМ ---
		{
			name:          "Прилагательное с супплетивизмом (хороший)",
			word:          "хороший",
			expectedLemma: "хороший",
			expectedPOS:   "Прилагательное",
			expectedCase:  "Именительный",
			expectedForms: []string{"хорош", "хороша", "хорошее", "хороший", "хорошую", "лучше", "лучший", "лучшая", "лучшую"},
		},
		{
			name:          "Супплетивная форма прилагательного (лучшая)",
			word:          "лучшая",
			expectedLemma: "хороший", // Лемма все равно "хороший"!
			expectedPOS:   "Прилагательное",
			expectedCase:  "Именительный",
			expectedForms: []string{"хорош", "хороша", "хорошее", "хороший", "хорошую", "лучше", "лучший", "лучшая", "лучшую"},
		},

		// --- ГЛАГОЛЫ ---
		{
			name:          "Глагол в инфинитиве (идти)",
			word:          "идти",
			expectedLemma: "идти",
			expectedPOS:   "Глагол",
			expectedCase:  "", // У глаголов нет падежа
			expectedForms: []string{"иди", "идите", "идти", "иду", "идёт", "идут", "шёл", "шла", "шли", "шедший"},
		},
		{
			name:          "Глагол в личной форме (иду)",
			word:          "иду",
			expectedLemma: "идти",
			expectedPOS:   "Глагол",
			expectedCase:  "",
			expectedForms: []string{"иди", "идите", "идти", "иду", "идёт", "идут", "шёл", "шла", "шли", "шедший"},
		},
		{
			name:          "Глагол в прошедшем времени (шёл)",
			word:          "шёл",
			expectedLemma: "идти",
			expectedPOS:   "Глагол",
			expectedCase:  "",
			expectedForms: []string{"иди", "идите", "идти", "иду", "идёт", "идут", "шёл", "шла", "шли", "шедший"},
		},

		// --- МЕСТОИМЕНИЯ ---
		{
			name:          "Личное местоимение (я)",
			word:          "я",
			expectedLemma: "я",
			expectedPOS:   "Местоимение",
			expectedCase:  "Именительный",
			expectedForms: []string{"я", "меня", "мне", "мной", "мною"},
		},
		{
			name:          "Личное местоимение не в начальной форме (ему)",
			word:          "ему",
			expectedLemma: "он",
			expectedPOS:   "Местоимение",
			expectedCase:  "Дательный",
			expectedForms: []string{"его", "ей", "ему", "ею", "её", "им", "ими", "их", "ней", "них", "нём", "он", "она", "они", "оно"},
		},

		// --- ЧИСЛИТЕЛЬНЫЕ ---
		{
			name:          "Числительное (двум)",
			word:          "двум",
			expectedLemma: "два",
			expectedPOS:   "Числительное",
			expectedCase:  "Дательный",
			expectedForms: []string{"два", "две", "двух", "двум", "двумя", "двух"},
		},

		// --- НАРЕЧИЯ (неизменяемые) ---
		{
			name:          "Наречие (быстро)",
			word:          "быстро",
			expectedLemma: "быстро",
			expectedPOS:   "Наречие",
			expectedCase:  "",
			expectedForms: []string{"быстро"},
		},

		// --- ПРИЧАСТИЯ ---
		{
			name:          "Причастие (сделавший)",
			word:          "сделавший",
			expectedLemma: "сделать", // Лемма - инфинитив глагола
			expectedPOS:   "Причастие",
			expectedCase:  "Именительный",
			// Причастия склоняются как прилагательные
			expectedForms: []string{"сделавший", "сделавшего", "сделавшему", "сделавшим", "сделавшем", "сделавшая", "сделавшую"},
		},

		// --- ДЕЕПРИЧАСТИЯ ---
		{
			name:          "Деепричастие (сделав)",
			word:          "сделав",
			expectedLemma: "сделать",
			expectedPOS:   "Деепричастие",
			expectedCase:  "",
			expectedForms: []string{"сделав", "сделавши"},
		},

		// --- СЛУЖЕБНЫЕ ЧАСТИ РЕЧИ (обычно неизменяемые) ---
		{
			name:          "Предлог (в)",
			word:          "в",
			expectedLemma: "в",
			expectedPOS:   "Предлог",
			expectedCase:  "",
			expectedForms: []string{"в"},
		},
		{
			name:          "Союз (и)",
			word:          "и",
			expectedLemma: "и",
			expectedPOS:   "Союз",
			expectedCase:  "",
			expectedForms: []string{"и"},
		},
		{
			name:          "Частица (не)",
			word:          "не",
			expectedLemma: "не",
			expectedPOS:   "Частица",
			expectedCase:  "",
			expectedForms: []string{"не"},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parses, forms := analyzer.Analyze(tc.word)

			if len(parses) == 0 {
				t.Fatalf("Слово '%s' не найдено в словаре, хотя должно было", tc.word)
			}

			foundParse := findParse(parses, tc.expectedLemma, tc.expectedPOS)
			if foundParse == nil {
				t.Fatalf("Ожидаемый разбор (лемма: %s, ЧР: %s) не найден", tc.expectedLemma, tc.expectedPOS)
			}

			if foundParse.Case != tc.expectedCase {
				t.Errorf("Неверный падеж: ожидали '%s', получили '%s'", tc.expectedCase, foundParse.Case)
			}

			actualForms := make([]string, len(forms))
			for i, p := range forms {
				actualForms[i] = p.Word
			}

			fmt.Println("actualForms", actualForms)

			for _, expectedForm := range tc.expectedForms {
				found := false
				for _, actualForm := range forms {
					if actualForm.Word == expectedForm {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Ожидаемая словоформа '%s' не найдена в сгенерированном списке", expectedForm)
				}
			}
		})
	}
}

func TestAnalyze_AmbiguousWord(t *testing.T) {
	word := "стали"
	parses, _ := analyzer.Analyze(word)

	if len(parses) < 2 {
		t.Fatalf("Для слова '%s' ожидалось как минимум 2 разбора (глагол и сущ.), получено %d", word, len(parses))
	}

	verbParse := findParse(parses, "стать", "Глагол")
	if verbParse == nil {
		t.Error("Не найден разбор для 'стали' как глагола 'стать'")
	}

	nounParse := findParse(parses, "сталь", "Существительное")
	if nounParse == nil {
		t.Error("Не найден разбор для 'стали' как существительного 'сталь'")
	} else {
		if nounParse.Case != "Родительный" {
			t.Errorf("Для 'стали' (сущ) ожидали Родительный падеж, получили '%s'", nounParse.Case)
		}
	}
}

// --- ТЕСТЫ ДЛЯ НЕСЛОВАРНЫХ СЛОВ (OOV) ---

func TestAnalyze_OOVWords(t *testing.T) {
	testCases := []struct {
		name                string
		word                string
		expectedLemma       string
		expectedPOS         string
		expectedForms       []string // Проверяем только наличие нескольких ключевых форм
		shouldBePredictable bool
	}{
		{
			name:                "Угадывание 1 (Существительное)",
			word:                "нейросетей",
			expectedLemma:       "нейросеть",
			expectedPOS:         "Существительное",
			expectedForms:       []string{"нейросеть", "нейросети", "нейросетью", "нейросетям", "нейросетями", "нейросетях"},
			shouldBePredictable: true,
		},
		{
			name:                "Угадывание 2 (Прилагательное)",
			word:                "скилловым",
			expectedLemma:       "скилловый",
			expectedPOS:         "Прилагательное",
			expectedForms:       []string{"скилловая", "скиллового", "скилловое", "скилловый", "скилловыми", "скилловых"},
			shouldBePredictable: true,
		},
		{
			name:                "Угадывание 3 (Глагол)",
			word:                "чекал",
			expectedLemma:       "чекать",
			expectedPOS:         "Глагол",
			expectedForms:       []string{"чекать", "чекает", "чекают", "чекайте", "чекаешь", "чекаете", "чекаем"},
			shouldBePredictable: true,
		},
		{
			name:                "Угадывание 4 (набор букв)",
			word:                "пкауйкйцк",
			expectedLemma:       "пкауйкйцк",
			expectedPOS:         "Существительное",
			expectedForms:       []string{"пкауйкйцк", "пкауйкйцка", "пкауйкйцками", "пкауйкйцках", "пкауйкйцку"},
			shouldBePredictable: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			parses, forms := analyzer.Analyze(tc.word)

			if !tc.shouldBePredictable {
				if parses != nil || forms != nil {
					t.Errorf("Слово '%s' не должно было быть разобрано, но результат получен", tc.word)
				}
				return // Успешно, переходим к следующему тесту
			}

			if parses == nil {
				t.Fatalf("Слово '%s' не было предсказано, хотя должно было", tc.word)
			}
			if len(parses) != 1 {
				t.Fatalf("Для предсказанного слова ожидается 1 вариант разбора, получено %d", len(parses))
			}

			p := parses[0]
			if p.Lemma != tc.expectedLemma {
				t.Errorf("Неверная предсказанная лемма: ожидали '%s', получили '%s'", tc.expectedLemma, p.Lemma)
			}
			if p.PartOfSpeech != tc.expectedPOS {
				t.Errorf("Неверная предсказанная ЧР: ожидали '%s', получили '%s'", tc.expectedPOS, p.PartOfSpeech)
			}

			// Проверяем, что ключевые словоформы были сгенерированы
			for _, expectedForm := range tc.expectedForms {
				found := false
				for _, actualForm := range forms {
					if actualForm.Word == expectedForm {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Ожидаемая словоформа '%s' не найдена в сгенерированном списке", expectedForm)
				}
			}
		})
	}
}

// TestParseList проверяет корректность работы метода пакетной обработки разбора слов.
func TestParseList(t *testing.T) {
	words := []string{"мама", "стали", "коту", "нейросети", "сёрчив"}

	// Ожидаемые леммы (включая оба варианта для "стали")
	expectedLemmas := map[string]bool{
		"мама":      true,
		"стать":     true,
		"сталь":     true,
		"кот":       true,
		"нейросеть": true,
		"сёрчить":   true,
	}

	results := analyzer.ParseList(words)

	// Проверяем, что количество результатов больше или равно количеству слов
	// (больше из-за омонимов).
	if len(results) < len(words) {
		t.Fatalf("Ожидалось как минимум %d разборов, получено %d", len(words), len(results))
	}

	// Проверяем, что все ожидаемые леммы присутствуют в результатах.
	foundLemmas := make(map[string]bool)
	for _, p := range results {
		foundLemmas[p.Lemma] = true
	}

	for lemma := range expectedLemmas {
		if !foundLemmas[lemma] {
			t.Errorf("Ожидаемая лемма '%s' не найдена в результатах пакетной обработки", lemma)
		}
	}

	// Проверяем, что результат отсортирован по слову
	isSorted := sort.SliceIsSorted(results, func(i, j int) bool {
		return results[i].Word < results[j].Word
	})
	if !isSorted {
		t.Error("Результат ParseList не отсортирован по полю Word")
	}
}

// TestInflectList проверяет корректность работы метода пакетной обработки поиска словоформ.
func TestInflectList(t *testing.T) {
	words := []string{"мама", "бежать", "нейросети", "лучший"}

	// Ожидаемые леммы (включая оба варианта для "стали")
	expectedForms := []string{
		"мам",
		"мама",
		"мамам",
		"мамах",
		"мамой",
		"бегут",
		"бегущий",
		"бежав",
		"бежавший",
		"бежим",
		"нейросетей",
		"нейросети",
		"нейросеть",
		"нейросетям",
		"нейросетях",
		"лучшую",
		"хороших",
		"наилучшая",
		"наилучшие",
		"наилучший",
	}

	results := analyzer.InflectList(words)

	// Проверяем, что количество результатов больше или равно количеству слов
	// (больше из-за омонимов).
	if len(results) < len(words) {
		t.Fatalf("Ожидалось как минимум %d разборов, получено %d", len(words), len(results))
	}

	// Проверяем, что все ожидаемые леммы присутствуют в результатах.
	for _, expectedForm := range expectedForms {
		found := false
		for _, actualForm := range results {
			if expectedForm == actualForm.Word {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Ожидаемая словоформа '%v' не найдена в сгенерированном списке", expectedForm)
		}
	}

	// Проверяем, что результат отсортирован по слову
	isSorted := sort.SliceIsSorted(results, func(i, j int) bool {
		return results[i].Word < results[j].Word
	})
	if !isSorted {
		t.Error("Результат ParseList не отсортирован по полю Word")
	}
}

// findParse ищет в срезе разборов тот, который соответствует ожиданиям.
// Необходимо для неоднозначных слов.
func findParse(parses []*steosmorphy.Parsed, lemma, pos string) *steosmorphy.Parsed {
	for _, p := range parses {
		if p.Lemma == lemma && p.PartOfSpeech == pos {
			return p
		}
	}
	return nil
}
