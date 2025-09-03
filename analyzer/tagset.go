// tagset.go определяет структуры и логику для работы с грамматическими тегами.
// Его основная задача — преобразовать компактную строку тегов
// в богатый, структурированный объект `Parsed`, с которым удобно работать
// как в самом Go, так и после сериализации в JSON для Python.
package analyzer

import (
	"strings"
)

// GrammemeSet - это множество для хранения грамматических тегов.
type GrammemeSet map[string]struct{}

// Parsed - это объект для хранения полного морфологического разбора.
type Parsed struct {
	Word         string      `json:"word"`           // Исходное слово
	Lemma        string      `json:"lemma"`          // Нормальная форма (лемма)
	Tags         string      `json:"tags"`           // Полная строка тегов для отладки
	PartOfSpeech string      `json:"part_of_speech"` // Часть речи
	Animacy      string      `json:"animacy"`        // Одушевленность
	Aspect       string      `json:"aspect"`         // Вид
	Case         string      `json:"case"`           // Падеж
	Gender       string      `json:"gender"`         // Род
	Mood         string      `json:"mood"`           // Наклонение
	Number       string      `json:"number"`         // Число
	Person       string      `json:"person"`         // Лицо
	Tense        string      `json:"tense"`          // Время
	Transitivity string      `json:"transitivity"`   // Переходность
	Voice        string      `json:"voice"`          // Залог
	OtherTags    GrammemeSet `json:"other_tags"`     // Остальные теги, не вошедшие в основные категории
}

// Глобальные переменные, содержащие множества всех возможных граммем для каждой категории.
// Они используются функцией `newParsed` для быстрой проверки, к какой категории относится тот или иной тег.
var (
	// posTags соответствует атрибуту "Часть речи" (ID 1)
	posTags = GrammemeSet{
		"Существительное": {},
		"Прилагательное":  {},
		"Глагол":          {},
		"Наречие":         {},
		"Причастие":       {},
		"Деепричастие":    {},
		"Местоимение":     {},
		"Числительное":    {},
		"Предлог":         {},
		"Частица":         {},
		"Союз":            {},
		"Междометие":      {},
		"Вводное слово":   {},
	}

	// animacyTags соответствует атрибуту "Одушевленность" (ID 2)
	animacyTags = GrammemeSet{
		"Одушевленное":                  {},
		"Неодушевленное":                {},
		"одушевленное и неодушевленное": {},
	}

	// aspectTags соответствует атрибуту "Вид глагола" (ID 11)
	aspectTags = GrammemeSet{
		"Совершенный":   {},
		"Несовершенный": {},
		"Двувидовой":    {},
	}

	// caseTags соответствует атрибуту "Падеж" (ID 6)
	caseTags = GrammemeSet{
		"Именительный": {},
		"Родительный":  {},
		"Дательный":    {},
		"Винительный":  {},
		"Творительный": {},
		"Предложный":   {},
		"Звательный":   {},
		"Местный":      {},
		"Счетный":      {},
		"Партитивный":  {},
		"Несклоняемый": {},
		"Ждательный":   {},
	}

	// genderTags соответствует атрибуту "Род" (ID 4)
	genderTags = GrammemeSet{
		"Мужской": {},
		"Женский": {},
		"Средний": {},
		"Общий":   {},
		"Парный":  {},
	}

	// moodTags соответствует атрибуту "Наклонение глагола" (ID 15)
	moodTags = GrammemeSet{
		"Повелительное": {},
	}

	// numberTags соответствует атрибутам "Число" (ID 5, 36)
	numberTags = GrammemeSet{
		"Единственное число":  {},
		"Множественное число": {},
	}

	// personTags соответствует атрибутам "Лицо глагола" (ID 17), "Лицо местоимения" (ID 29)
	personTags = GrammemeSet{
		"1-е лицо": {},
		"2-е лицо": {},
		"3-е лицо": {},
		"нет лица": {},
	}

	// tenseTags соответствует атрибуту "Время глагола" (ID 14)
	tenseTags = GrammemeSet{
		"Прошедшее":             {},
		"Настоящее":             {},
		"Будущее":               {},
		"Будущее аналитическое": {},
	}

	// transTags соответствует атрибуту "Переходность глагола" (ID 12)
	transTags = GrammemeSet{
		"Переходный":   {},
		"Непереходный": {},
		"Лабильный":    {},
	}

	// voiceTags соответствует атрибуту "Залог причастия" (ID 18)
	voiceTags = GrammemeSet{
		"Действительный": {},
		"Страдательный":  {},
	}
)

// newParsed - это конструктор-фабрика для объекта `Parsed`.
// Он принимает "сырые" данные (слово, лемму и строку тегов) и возвращает
// полностью заполненный, структурированный объект.
func newParsed(word, lemma, tagString string) *Parsed {
	// Создаем базовый объект с основными данными.
	p := &Parsed{Word: word, Lemma: lemma, Tags: tagString, OtherTags: make(GrammemeSet)}

	// Разбиваем строку тегов на отдельные граммемы.
	grammemes := strings.Split(tagString, ",")

	// Обрабатываем `Часть Речи` отдельно, так как она всегда идет первой.
	if len(grammemes) > 0 {
		if _, ok := posTags[grammemes[0]]; ok {
			p.PartOfSpeech = grammemes[0]
		}
	}

	// Проходим по всем граммемам и раскладываем их по соответствующим полям структуры `Parsed`.
	for _, g := range grammemes {
		switch {
		case g == p.PartOfSpeech: // пропускаем, так как уже обработали.
		case inMap(g, animacyTags):
			p.Animacy = g
		case inMap(g, aspectTags):
			p.Aspect = g
		case inMap(g, caseTags):
			p.Case = g
		case inMap(g, genderTags):
			p.Gender = g
		case inMap(g, moodTags):
			p.Mood = g
		case inMap(g, numberTags):
			p.Number = g
		case inMap(g, personTags):
			p.Person = g
		case inMap(g, tenseTags):
			p.Tense = g
		case inMap(g, transTags):
			p.Transitivity = g
		case inMap(g, voiceTags):
			p.Voice = g
		default:
			// Если тег не подошел ни к одной из основных категорий,
			// мы помещаем его в "корзину" OtherTags.
			p.OtherTags[g] = struct{}{}
		}
	}
	return p
}

func inMap(key string, set GrammemeSet) bool {
	_, ok := set[key]
	return ok
}
