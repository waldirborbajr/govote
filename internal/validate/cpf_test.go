package validate

import "testing"

// CPFs de exemplo amplamente usados em testes; os dígitos verificadores
// foram conferidos com o mesmo algoritmo da função CPF.
func TestCPF(t *testing.T) {
	cases := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valido formatado", "529.982.247-25", false},
		{"valido so digitos", "52998224725", false},
		{"valido com espacos", "  529.982.247-25 ", false},
		{"digito verificador errado", "529.982.247-26", true},
		{"sequencia repetida", "111.111.111-11", true},
		{"tamanho curto", "1234567890", true},
		{"tamanho longo", "529982247250", true},
		{"letras", "529.982.247-2A", true},
		{"vazio", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := CPF(tc.in)
			if (err != nil) != tc.wantErr {
				t.Fatalf("CPF(%q) erro=%v, esperado erro=%v", tc.in, err, tc.wantErr)
			}
		})
	}
}
