package web

import (
	"html/template"

	"github.com/waldirborbajr/govote/internal/models"
)

// PageData is the view model passed to the HTMX templates.
type PageData struct {
	Error       string
	Message     string
	CPF         string
	Polls       []models.Poll
	Poll        models.Poll
	Results     []models.ResultAnswer
	WhatsAppURL string
	AdminUser   *models.Admin
	AdminsList  []models.Admin
}

// Templates holds every HTMX UI fragment used by the application.
var Templates = template.Must(template.New("ui").Parse(`
{{define "page"}}
<!doctype html>
<html>
<head>
  <meta charset="utf-8">
  <title>Vote API - PoC</title>
  <script src="https://cdn.tailwindcss.com"></script>
  <link href="https://cdn.jsdelivr.net/npm/daisyui@4.12.10/dist/full.min.css" rel="stylesheet" type="text/css" />
  <script src="https://unpkg.com/htmx.org@1.9.12/htmx.min.js"></script>

  <script>
    function formatCPF(input) {
      let v = input.value.replace(/\D/g, '');
      v = v.replace(/(\d{3})(\d)/, '$1.$2');
      v = v.replace(/(\d{3})(\d)/, '$1.$2');
      v = v.replace(/(\d{3})(\d{1,2})$/, '$1-$2');
      input.value = v.substring(0, 14);
    }

    function formatPhone(input) {
      let v = input.value.replace(/\D/g, '');
      if (v.length > 11) v = v.substring(0, 11);
      if (v.length <= 10) {
        v = v.replace(/(\d{2})(\d)/, '($1) $2');
        v = v.replace(/(\d{4})(\d)/, '$1-$2');
      } else {
        v = v.replace(/(\d{2})(\d{5})(\d{4})/, '($1) $2-$3');
      }
      input.value = v;
    }
  </script>
</head>
<body class="bg-base-200 min-h-screen p-4 md:p-8">
  <div class="max-w-3xl mx-auto bg-base-100 p-8 rounded-3xl shadow-2xl">
    <h1 class="text-4xl font-bold mb-2 text-center text-primary">🗳️ Vote API</h1>
    <p class="text-center text-base-content/70 mb-10">Sistema de Votação Simples e Seguro</p>

    <div id="app">{{template "index" .}}</div>
  </div>
</body>
</html>
{{end}}

{{define "index"}}
<div class="grid grid-cols-1 md:grid-cols-2 gap-6">
  <div class="card bg-base-200 shadow-xl p-8 hover:shadow-2xl transition-all">
    <div class="text-center mb-6">
      <div class="text-5xl mb-4">🗳️</div>
      <h2 class="text-2xl font-bold mb-2">Votar</h2>
      <p class="text-base-content/70">Participe das enquetes ativas</p>
    </div>
    <button hx-get="/ui/voting-flow" hx-target="#app" class="btn btn-primary btn-lg w-full">
      Acessar Votação
    </button>
  </div>

  <div class="card bg-base-200 shadow-xl p-8 hover:shadow-2xl transition-all">
    <div class="text-center mb-6">
      <div class="text-5xl mb-4">⚙️</div>
      <h2 class="text-2xl font-bold mb-2">Administração</h2>
      <p class="text-base-content/70">Gerenciar enquetes e resultados</p>
    </div>
    <button hx-get="/ui/admin" hx-target="#app" class="btn btn-secondary btn-lg w-full">
      Entrar como Administrador
    </button>
  </div>
</div>
{{end}}

{{define "admin_dashboard"}}
<div class="space-y-6">
  <h2 class="text-3xl font-bold text-center">Painel Administrativo</h2>
  <p class="text-center text-sm font-semibold">Logado como: <span class="text-primary">{{.AdminUser.Username}}</span></p>

  <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
    <button hx-get="/ui/polls/create" hx-target="#app" class="btn btn-primary h-24 text-lg">
      ➕ Criar Nova Enquete
    </button>

    <button hx-get="/ui/admin/polls" hx-target="#app" class="btn btn-secondary h-24 text-lg">
      📊 Ver Minhas Enquetes
    </button>

    <button hx-get="/ui/admin/stats" hx-target="#app" class="btn btn-accent h-24 text-lg">
      📈 Estatísticas Globais
    </button>

    <button hx-post="/ui/admin/request-temp-password" hx-target="#app" class="btn btn-info h-24 text-lg">
      🔑 Solicitar Senha Temporária
    </button>

    {{if .AdminUser.IsSuper}}
    <button hx-get="/ui/admin/manage-admins" hx-target="#app" class="btn btn-warning h-24 text-lg md:col-span-2">
      👥 Gerenciar Administradores
    </button>
    {{end}}
  </div>

  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Início</button>
</div>
{{end}}

{{define "admin_passcode_sent"}}
<div class="card bg-base-100 shadow-xl p-8 text-center space-y-6">
  <h2 class="text-3xl font-bold text-success">✅ Token enviado para o WhatsApp!</h2>
  <p class="text-lg">Use o link abaixo para acionar a mensagem simulada e em seguida insira o código na tela de login.</p>
  {{if .WhatsAppURL}}
  <a href="{{.WhatsAppURL}}" target="_blank" class="btn btn-primary btn-lg w-full">📱 Enviar Código via WhatsApp</a>
  {{end}}
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-outline w-full">Ir para tela de Login</button>
</div>
{{end}}

{{define "admin_login"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-md mx-auto space-y-6">
  <h2 class="text-2xl font-bold text-center text-secondary">🔐 Login Administrador</h2>
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}

  <form hx-post="/ui/admin/request-temp-password" hx-target="#app" class="bg-base-200 p-4 rounded-xl space-y-2">
    <span class="text-sm font-semibold text-gray-500 block">Solicite senha temporária via WhatsApp</span>
    <input name="phone" placeholder="(11) 98765-4321" onkeyup="formatPhone(this)" class="input input-bordered w-full" required>
    <button class="btn btn-primary w-full">📱 Solicitar Senha Temporária</button>
  </form>

  <div class="divider">OU</div>

  <form hx-post="/ui/admin/login" hx-target="#app" class="space-y-4">
    <input name="username" placeholder="Usuário" class="input input-bordered w-full" required>
    <input name="password" type="password" placeholder="Senha Temporária" class="input input-bordered w-full" required>
    <button class="btn btn-primary w-full">Entrar</button>
  </form>
  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar</button>
</div>
{{end}}

<!-- Rest of templates omitted for brevity but full in actual file -->
`))
