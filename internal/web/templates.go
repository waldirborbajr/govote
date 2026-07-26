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
  <script src="/static/htmx.min.js"></script>

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

{{define "admin_change_password"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-md mx-auto space-y-6">
  <h2 class="text-2xl font-bold text-center text-warning">🔑 Definir Nova Senha</h2>
  <p class="text-center text-sm text-base-content/70">Este é o seu primeiro acesso ou sua senha foi redefinida — escolha uma nova senha (mínimo 8 caracteres).</p>
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
  <form hx-post="/ui/admin/change-password" hx-target="#app" class="space-y-4">
    <input type="hidden" name="username" value="{{.AdminUser.Username}}">
    <input name="new_password" type="password" placeholder="Nova senha" minlength="8" class="input input-bordered w-full" required>
    <button class="btn btn-warning w-full">Salvar nova senha</button>
  </form>
</div>
{{end}}

{{define "auth"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-md mx-auto space-y-6">
  <h2 class="text-2xl font-bold text-center text-primary">🗳️ Acesso do Eleitor</h2>
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}

  <form hx-post="/ui/request-passcode" hx-target="#app" class="bg-base-200 p-4 rounded-xl space-y-2">
    <span class="text-sm font-semibold text-gray-500 block">Solicitar código de votação</span>
    <input name="cpf" placeholder="000.000.000-00" onkeyup="formatCPF(this)" class="input input-bordered w-full" required>
    <input name="name" placeholder="Nome completo" class="input input-bordered w-full" required>
    <div class="flex gap-2">
      <input name="country_code" placeholder="+55" value="+55" class="input input-bordered w-20" required>
      <input name="phone" placeholder="(11) 98765-4321" onkeyup="formatPhone(this)" class="input input-bordered w-full" required>
    </div>
    <button class="btn btn-primary w-full">📱 Solicitar Código</button>
  </form>

  <div class="divider">JÁ TENHO UM CÓDIGO</div>

  <form hx-post="/ui/verify" hx-target="#app" class="space-y-2">
    <input name="cpf" placeholder="000.000.000-00" onkeyup="formatCPF(this)" class="input input-bordered w-full" required>
    <input name="passcode" placeholder="Código recebido" maxlength="4" class="input input-bordered w-full" required>
    <button class="btn btn-secondary w-full">✅ Verificar Código</button>
  </form>

  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar</button>
</div>
{{end}}

{{define "verify_form"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-md mx-auto space-y-6">
  <h2 class="text-2xl font-bold text-center text-secondary">✅ Verificar Código</h2>
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
  <form hx-post="/ui/verify" hx-target="#app" class="space-y-2">
    <input name="cpf" placeholder="000.000.000-00" onkeyup="formatCPF(this)" class="input input-bordered w-full" required>
    <input name="passcode" placeholder="Código recebido" maxlength="4" class="input input-bordered w-full" required>
    <button class="btn btn-secondary w-full">Verificar</button>
  </form>
  <button hx-get="/ui/voting-flow" hx-target="#app" class="btn btn-ghost w-full">← Voltar</button>
</div>
{{end}}

{{define "voting_flow"}}
<div class="space-y-6 max-w-md mx-auto">
  <h2 class="text-2xl font-bold text-center">Como você quer continuar?</h2>
  <button hx-get="/ui/request-passcode-form" hx-target="#app" class="btn btn-primary btn-lg w-full">
    📱 Solicitar código de votação
  </button>
  <button hx-get="/ui/verify-form" hx-target="#app" class="btn btn-secondary btn-lg w-full">
    ✅ Já tenho um código
  </button>
  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Início</button>
</div>
{{end}}

{{define "passcode_sent"}}
<div class="card bg-base-100 shadow-xl p-8 text-center space-y-6 max-w-md mx-auto">
  <h2 class="text-3xl font-bold text-success">✅ Código enviado!</h2>
  <p class="text-lg">Use o link abaixo para receber o código via WhatsApp e depois insira-o na tela de verificação.</p>
  {{if .WhatsAppURL}}
  <a href="{{.WhatsAppURL}}" target="_blank" class="btn btn-primary btn-lg w-full">📱 Enviar Código via WhatsApp</a>
  {{end}}
  <button hx-get="/ui/verify-form" hx-target="#app" class="btn btn-outline w-full">Já recebi, verificar código</button>
</div>
{{end}}

{{define "polls"}}
<div class="space-y-6">
  {{if .AdminUser}}
    <h2 class="text-3xl font-bold text-center">📊 Enquetes</h2>
  {{else}}
    <h2 class="text-3xl font-bold text-center">🗳️ Enquetes Ativas</h2>
  {{end}}

  {{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}

  {{if not .Polls}}
    <p class="text-center text-base-content/70">Nenhuma enquete encontrada.</p>
  {{end}}

  <div class="grid grid-cols-1 gap-4">
    {{$cpf := .CPF}}
    {{range .Polls}}
    <div class="card bg-base-200 shadow p-6">
      <h3 class="text-xl font-bold">{{.Title}}</h3>
      <p class="text-sm text-base-content/70">Tipo: {{.Type}} · Início: {{.StartDate}} · Fim: {{.EndDate}}</p>
      <div class="mt-4 flex gap-2">
        {{if $.AdminUser}}
        <button hx-get="/ui/polls/{{.ID}}/results" hx-target="#app" class="btn btn-accent btn-sm">📈 Ver Resultados</button>
        {{else}}
        <button hx-get="/ui/polls/{{.ID}}?cpf={{$cpf}}" hx-target="#app" class="btn btn-primary btn-sm">Votar</button>
        {{end}}
      </div>
    </div>
    {{end}}
  </div>

  {{if .AdminUser}}
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Painel</button>
  {{else}}
  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Início</button>
  {{end}}
</div>
{{end}}

{{define "poll_detail"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-lg mx-auto space-y-6">
  <h2 class="text-2xl font-bold text-center">{{.Poll.Title}}</h2>
  <form hx-post="/ui/polls/{{.Poll.ID}}/vote" hx-target="#app" class="space-y-3">
    <input type="hidden" name="cpf" value="{{.CPF}}">
    {{range .Poll.Answers}}
    <label class="flex items-center gap-3 bg-base-200 p-3 rounded-xl cursor-pointer">
      {{if eq $.Poll.Type "radio"}}
      <input type="radio" name="answer_ids" value="{{.ID}}" class="radio radio-primary" required>
      {{else}}
      <input type="checkbox" name="answer_ids" value="{{.ID}}" class="checkbox checkbox-primary">
      {{end}}
      <span>{{.Text}}</span>
    </label>
    {{end}}
    <button class="btn btn-primary w-full">Confirmar Voto</button>
  </form>
  <button hx-get="/ui/polls?cpf={{.CPF}}" hx-target="#app" class="btn btn-ghost w-full">← Voltar às enquetes</button>
</div>
{{end}}

{{define "vote_result"}}
<div class="card bg-base-100 shadow-xl p-8 text-center space-y-6 max-w-md mx-auto">
  {{if .Error}}
  <h2 class="text-2xl font-bold text-error">❌ Não foi possível votar</h2>
  <p>{{.Error}}</p>
  {{else}}
  <h2 class="text-2xl font-bold text-success">✅ Voto registrado com sucesso!</h2>
  <p>Obrigado por participar.</p>
  {{end}}
  <button hx-get="/ui/polls?cpf={{.CPF}}" hx-target="#app" class="btn btn-outline w-full">← Voltar às enquetes</button>
</div>
{{end}}

{{define "results"}}
<div class="space-y-6 max-w-lg mx-auto">
  <h2 class="text-2xl font-bold text-center">📈 {{.Poll.Title}}</h2>
  {{if not .Results}}
  <p class="text-center text-base-content/70">Ainda não há votos para esta enquete.</p>
  {{end}}
  <div class="space-y-3">
    {{range .Results}}
    <div class="bg-base-200 p-4 rounded-xl flex justify-between items-center">
      <span>{{.Text}}</span>
      <span class="badge badge-primary badge-lg">{{.Votes}} voto(s)</span>
    </div>
    {{end}}
  </div>
  <button hx-get="/ui/admin/polls" hx-target="#app" class="btn btn-ghost w-full">← Voltar às enquetes</button>
</div>
{{end}}

{{define "create_poll"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-lg mx-auto space-y-4">
  <h2 class="text-2xl font-bold text-center">➕ Nova Enquete</h2>
  <form hx-post="/ui/polls/create" hx-target="#app" class="space-y-3">
    <input name="title" placeholder="Título da enquete" class="input input-bordered w-full" required>

    <select name="type" class="select select-bordered w-full" required>
      <option value="radio">Escolha única (radio)</option>
      <option value="checkbox">Múltipla escolha (checkbox)</option>
    </select>

    <label class="text-sm font-semibold block">Início</label>
    <input name="start_date" type="datetime-local" class="input input-bordered w-full" required>

    <label class="text-sm font-semibold block">Fim</label>
    <input name="end_date" type="datetime-local" class="input input-bordered w-full" required>

    <label class="flex items-center gap-2">
      <input type="checkbox" name="allow_blank" value="true" class="checkbox">
      <span class="text-sm">Permitir voto em branco</span>
    </label>

    <label class="text-sm font-semibold block">Respostas (uma por linha)</label>
    <textarea name="answers" rows="5" class="textarea textarea-bordered w-full" placeholder="Opção 1&#10;Opção 2&#10;Opção 3" required></textarea>

    <button class="btn btn-primary w-full">Publicar Enquete</button>
  </form>
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Painel</button>
</div>
{{end}}

{{define "global_stats"}}
<div class="space-y-6 max-w-lg mx-auto" id="global-stats-root">
  <h2 class="text-2xl font-bold text-center">📈 Estatísticas Globais</h2>
  <div id="global-stats-content" class="text-center text-base-content/70">Carregando...</div>
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Painel</button>
</div>
<script>
  (function () {
    fetch('/admin/stats')
      .then(function (r) { return r.json(); })
      .then(function (data) {
        var el = document.getElementById('global-stats-content');
        if (!el) return;
        var html = '<div class="stats shadow w-full">' +
          '<div class="stat"><div class="stat-title">Votos totais</div><div class="stat-value">' + data.total_votes + '</div></div>' +
          '<div class="stat"><div class="stat-title">Comparecimento</div><div class="stat-value">' + data.turnout_pct.toFixed(1) + '%</div></div>' +
          '</div>';
        el.innerHTML = html;
      })
      .catch(function () {
        var el = document.getElementById('global-stats-content');
        if (el) el.textContent = 'Erro ao carregar estatísticas.';
      });
  })();
</script>
{{end}}

{{define "manage_admins"}}
<div class="space-y-6">
  <h2 class="text-3xl font-bold text-center">👥 Gerenciar Administradores</h2>
  {{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}

  <form hx-post="/ui/admin/manage-admins" hx-target="#app" class="card bg-base-200 p-6 space-y-2">
    <span class="text-sm font-semibold text-gray-500 block">Adicionar / Atualizar Administrador</span>
    <input name="name" placeholder="Nome completo" class="input input-bordered w-full" required>
    <input name="cpf" placeholder="CPF (usado como usuário)" onkeyup="formatCPF(this)" class="input input-bordered w-full" required>
    <input name="phone" placeholder="(11) 98765-4321" onkeyup="formatPhone(this)" class="input input-bordered w-full" required>
    <label class="flex items-center gap-2">
      <input type="checkbox" name="enabled" value="true" class="checkbox" checked>
      <span class="text-sm">Ativo</span>
    </label>
    <button class="btn btn-primary w-full">Salvar</button>
  </form>

  <div class="space-y-2">
    {{range .AdminsList}}
    <div class="bg-base-200 p-4 rounded-xl flex justify-between items-center">
      <div>
        <div class="font-bold">{{.Name}} {{if .IsSuper}}<span class="badge badge-warning badge-sm">super</span>{{end}}</div>
        <div class="text-xs text-base-content/70">{{.Username}} · {{.Phone}}</div>
      </div>
      <span class="badge {{if .Enabled}}badge-success{{else}}badge-error{{end}}">{{if .Enabled}}ativo{{else}}desativado{{end}}</span>
    </div>
    {{end}}
  </div>

  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Painel</button>
</div>
{{end}}
`))
