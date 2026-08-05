// Package validate contains shared request-payload validation helpers.
package validate

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

var (
	reDigitsOnly = regexp.MustCompile(`^\d+$`)
	rePhoneBR    = regexp.MustCompile(`^\+?55\d{10,11}$|^\d{10,11}$`)
)

// CPF valida formato (11 dígitos) e dígitos verificadores.
func CPF(cpf string) error {
	cpf = strings.TrimSpace(cpf)
	cpf = strings.ReplaceAll(strings.ReplaceAll(cpf, ".", ""), "-", "")

	if len(cpf) != 11 || !reDigitsOnly.MatchString(cpf) {
		return fmt.Errorf("cpf deve ter 11 dígitos")
	}

	// rejeita sequências óbvias (111.111.111-11 etc.)
	allSame := true
	for i := 1; i < 11; i++ {
		if cpf[i] != cpf[0] {
			allSame = false
			break
		}
	}
	if allSame {
		return fmt.Errorf("cpf inválido")
	}

	if !cpfCheckDigits(cpf) {
		return fmt.Errorf("cpf inválido")
	}
	return nil
}

func cpfCheckDigits(cpf string) bool {
	sum := 0
	for i := 0; i < 9; i++ {
		d, _ := strconv.Atoi(string(cpf[i]))
		sum += d * (10 - i)
	}
	r := sum % 11
	d1 := 0
	if r >= 2 {
		d1 = 11 - r
	}
	if d1 != int(cpf[9]-'0') {
		return false
	}

	sum = 0
	for i := 0; i < 10; i++ {
		d, _ := strconv.Atoi(string(cpf[i]))
		sum += d * (11 - i)
	}
	r = sum % 11
	d2 := 0
	if r >= 2 {
		d2 = 11 - r
	}
	return d2 == int(cpf[10]-'0')
}

// Name exige entre 2 e 120 caracteres (após trim) e pelo menos uma letra.
func Name(name string) error {
	name = strings.TrimSpace(name)
	if len(name) < 2 || len(name) > 120 {
		return fmt.Errorf("nome deve ter entre 2 e 120 caracteres")
	}
	hasLetter := false
	for _, r := range name {
		if unicode.IsLetter(r) {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return fmt.Errorf("nome inválido")
	}
	return nil
}

// Phone aceita formato BR comum: 10/11 dígitos ou +55...
func Phone(phone string) error {
	phone = strings.TrimSpace(phone)
	phone = strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(phone, "(", ""), ")", ""), "-", ""), " ", "")
	if !rePhoneBR.MatchString(phone) {
		return fmt.Errorf("telefone inválido (use DDD + número, ex: 41999999999 ou +5541999999999)")
	}
	return nil
}

// Passcode exige exatamente 4 dígitos.
func Passcode(code string) error {
	code = strings.TrimSpace(code)
	if len(code) != 4 || !reDigitsOnly.MatchString(code) {
		return fmt.Errorf("código deve ter 4 dígitos")
	}
	return nil
}

// PollTitle ...
func PollTitle(title string) error {
	title = strings.TrimSpace(title)
	if len(title) < 3 || len(title) > 200 {
		return fmt.Errorf("título deve ter entre 3 e 200 caracteres")
	}
	return nil
}

// PollType só "radio" ou "checkbox".
func PollType(t string) error {
	if t != "radio" && t != "checkbox" {
		return fmt.Errorf("type deve ser radio ou checkbox")
	}
	return nil
}

// RFC3339Date parseia e retorna o time.
func RFC3339Date(s, field string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("%s é obrigatório", field)
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s deve estar no formato RFC3339", field)
	}
	return t, nil
}

// AnswerTexts valida lista de respostas.
func AnswerTexts(answers []string) error {
	if len(answers) < 2 {
		return fmt.Errorf("informe pelo menos 2 respostas")
	}
	if len(answers) > 20 {
		return fmt.Errorf("máximo de 20 respostas")
	}
	seen := make(map[string]struct{}, len(answers))
	for i, a := range answers {
		a = strings.TrimSpace(a)
		if a == "" {
			return fmt.Errorf("resposta %d vazia", i+1)
		}
		if len(a) > 200 {
			return fmt.Errorf("resposta %d muito longa (máx 200)", i+1)
		}
		key := strings.ToLower(a)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("respostas duplicadas")
		}
		seen[key] = struct{}{}
	}
	return nil
}
