#include <stdio.h>
#include <stdlib.h>
#include "libanalyzer.h"

int main() {
    char* word = "программисткою";
    char* lemma;

    CreateAnalyzer();

    lemma = AnalyzeWord(word);

    if (lemma != NULL) {
        printf("Слово: %s\nЛемма: %s\n", word, lemma);
        FreeString(lemma);
    } else {
        printf("Не удалось проанализировать слово: %s\n", word);
    }

    ReleaseAnalyzer();

    return 0;
}