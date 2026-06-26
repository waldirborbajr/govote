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
  <script src="https://unpkg.com/htmx.org@1.9.12"></script>

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

{{define "voting_flow"}}
<div class="card bg-base-100 shadow-xl p-8">
  <h2 class="text-2xl font-bold mb-6 text-center">🗳️ Área de Votação</h2>
  {{if .Error}}<div class="alert alert-error mb-6">{{.Error}}</div>{{end}}

  <div class="grid gap-4">
    <button hx-get="/ui/request-passcode-form" hx-target="#app" class="btn btn-primary btn-lg">
      📱 Gerar Código de Acesso
    </button>
    <div class="divider">OU</div>
    <button hx-get="/ui/verify-form" hx-target="#app" class="btn btn-outline btn-lg">
      🔑 Já tenho código (Entrar)
    </button>
  </div>
  <button hx-get="/" hx-target="#app" class="btn btn-ghost mt-8 w-full">← Voltar</button>
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

    {{if .AdminUser.IsSuper}}
    <button hx-get="/ui/admin/manage-admins" hx-target="#app" class="btn btn-warning h-24 text-lg md:col-span-2">
      👥 Gerenciar Administradores
    </button>
    {{end}}
  </div>

  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Início</button>
</div>
{{end}}

{{define "manage_admins"}}
<div class="space-y-6">
  <h2 class="text-2xl font-bold text-center text-warning">👥 Gerenciar Administradores</h2>
  {{if .Error}}<div class="alert alert-error mb-4">{{.Error}}</div>{{end}}
  {{if .Message}}<div class="alert alert-success mb-4">{{.Message}}</div>{{end}}

  <form hx-post="/ui/admin/manage-admins" hx-target="#app" class="card bg-base-200 p-6 space-y-4 shadow-md">
    <h3 class="text-lg font-bold">Cadastrar Novo Administrador</h3>
    <div class="grid grid-cols-1 md:grid-cols-2 gap-4">
      <div class="form-control">
        <label class="label"><span class="label-text">Nome</span></label>
        <input name="name" placeholder="Nome Completo" class="input input-bordered" required>
      </div>
      <div class="form-control">
        <label class="label"><span class="label-text">CPF</span></label>
        <input name="cpf" placeholder="000.000.000-00" onkeyup="formatCPF(this)" class="input input-bordered" maxlength="14" required>
      </div>
      <div class="form-control">
        <label class="label"><span class="label-text">Celular</span></label>
        <input name="phone" placeholder="(11) 98765-4321" onkeyup="formatPhone(this)" class="input input-bordered" maxlength="15" required>
      </div>
      <div class="form-control">
        <label class="label"><span class="label-text">Status Inicial</span></label>
        <select name="enabled" class="select select-bordered">
          <option value="true" selected>Ativo (True)</option>
          <option value="false">Inativo (False)</option>
        </select>
      </div>
    </div>
    <button type="submit" class="btn btn-primary w-full mt-2">Salvar Novo Admin</button>
  </form>

  <div class="card bg-base-100 p-6 shadow-md overflow-x-auto">
    <h3 class="text-lg font-bold mb-4">Administradores Existentes</h3>
    <table class="table table-zebra w-full">
      <thead>
        <tr>
          <th>Nome</th>
          <th>CPF / Usuário</th>
          <th>Celular</th>
          <th>Função</th>
          <th>Status</th>
        </tr>
      </thead>
      <tbody>
        {{range .AdminsList}}
        <tr>
          <td>{{.Name}}</td>
          <td>{{.Username}}</td>
          <td>{{.Phone}}</td>
          <td>{{if .IsSuper}}<span class="badge badge-error">Super Admin</span>{{else}}<span class="badge badge-ghost">Normal</span>{{end}}</td>
          <td>{{if .Enabled}}<span class="text-success font-bold">Ativo</span>{{else}}<span class="text-error font-bold">Inativo</span>{{end}}</td>
        </tr>
        {{end}}
      </tbody>
    </table>
  </div>

  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full">← Voltar ao Painel</button>
</div>
{{end}}

{{define "passcode_sent"}}
<div class="card bg-base-100 shadow-xl p-8 text-center space-y-6">
  <h2 class="text-3xl font-bold text-success">✅ Código Gerado!</h2>
  <p class="text-lg">Envie o código pelo WhatsApp para continuar.</p>
  {{if .WhatsAppURL}}
  <a href="{{.WhatsAppURL}}" target="_blank" class="btn btn-primary btn-lg w-full">📱 Abrir WhatsApp</a>
  {{end}}
  <button hx-get="/ui/voting-flow" hx-target="#app" class="btn btn-outline w-full">Voltar</button>
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

{{define "auth"}}
{{if .Error}}<div class="alert alert-error mb-6 shadow-sm">{{.Error}}</div>{{end}}
<div class="grid gap-8">
  <form hx-post="/ui/request-passcode" hx-target="#app" hx-swap="innerHTML" class="card bg-base-200 p-6 space-y-4">
    <h2 class="text-xl font-bold">1. Solicitar Acesso</h2>
    <div class="form-control">
      <label class="label"><span class="label-text">CPF</span></label>
      <input name="cpf" id="cpf" placeholder="000.000.000-00" class="input input-bordered w-full" maxlength="14" onkeyup="formatCPF(this)" required>
    </div>
    <div class="form-control">
      <label class="label"><span class="label-text">Nome Completo</span></label>
      <input name="name" placeholder="Nome" class="input input-bordered w-full" required>
    </div>
    <div class="form-control">
      <label class="label"><span class="label-text">Celular (com DDD)</span></label>
      <div class="join w-full">
        <select name="country_code" class="select select-bordered join-item w-28">
          <option value="55" selected>Brasil (+55)</option>
          <option value="1">EUA/Canadá (+1)</option>
        </select>
        <input name="phone" id="phone" placeholder="(11) 98765-4321" class="input input-bordered join-item flex-1" onkeyup="formatPhone(this)" maxlength="15" required>
      </div>
    </div>
    <button class="btn btn-primary w-full">Gerar Código de Acesso</button>
  </form>

  <form hx-post="/ui/verify" hx-target="#app" hx-swap="innerHTML" class="card bg-base-200 p-6 space-y-4">
    <h2 class="text-xl font-bold">2. Verificar</h2>
    <input name="cpf" placeholder="CPF" class="input input-bordered w-full" required>
    <input name="passcode" placeholder="Passcode" class="input input-bordered w-full" required>
    <button class="btn btn-secondary w-full">Entrar</button>
  </form>
</div>
{{end}}

{{define "poll_detail"}}
<form hx-post="/ui/polls/{{.Poll.ID}}/vote" hx-target="#app" class="space-y-6">
  <input type="hidden" name="cpf" value="{{.CPF}}">
  <h2 class="text-2xl font-bold">{{.Poll.Title}}</h2>
  <div class="form-control gap-3">
    {{$type := .Poll.Type}}
    {{range .Poll.Answers}}
    <label class="label cursor-pointer justify-start gap-4 border p-4 rounded-lg hover:bg-base-200">
      <input type="{{if eq $type "radio"}}radio{{else}}checkbox{{end}}" name="answer_ids" value="{{.ID}}" class="{{if eq $type "radio"}}radio{{else}}checkbox{{end}}">
      <span class="label-text text-lg">{{.Text}}</span>
    </label>
    {{end}}
  </div>
  <button class="btn btn-success w-full">Confirmar Voto</button>
</form>
{{end}}

{{define "vote_result"}}
<div class="card bg-base-100 shadow-xl p-8 text-center space-y-6">
  {{if .Error}}
  <h2 class="text-2xl font-bold text-error">⚠️ Não foi possível registrar seu voto</h2>
  <div class="alert alert-error">{{.Error}}</div>
  {{else}}
  <h2 class="text-3xl font-bold text-success">✅ Voto registrado!</h2>
  <p class="text-lg">Obrigado por participar.</p>
  {{end}}
  <button hx-get="/ui/polls?cpf={{.CPF}}" hx-target="#app" class="btn btn-outline w-full">Voltar às enquetes</button>
</div>
{{end}}

{{define "results"}}
<div class="space-y-6">
  <h2 class="text-2xl font-bold">Resultados: {{.Poll.Title}}</h2>
  <div class="overflow-x-auto">
    <table class="table table-zebra w-full">
      <thead><tr><th>Opção</th><th>Votos</th></tr></thead>
      <tbody>
        {{range .Results}}<tr><td>{{.Text}}</td><td class="font-bold">{{.Votes}}</td></tr>{{end}}
      </tbody>
    </table>
  </div>
  <button hx-get="/ui/admin/polls" hx-target="#app" class="btn btn-ghost w-full">Voltar</button>
</div>
{{end}}

{{define "create_poll"}}
<div class="card bg-base-100 shadow-xl p-6 max-w-lg mx-auto">
  <h2 class="text-2xl font-bold mb-6 text-primary">Criar Nova Enquete</h2>
  <form hx-post="/ui/polls/create" hx-target="#app" class="space-y-4">
    <div class="form-control">
      <label class="label"><span class="label-text">Título</span></label>
      <input name="title" placeholder="Ex: Votação da CIPA" class="input input-bordered w-full" required>
    </div>

    <div class="grid grid-cols-2 gap-4">
      <div class="form-control">
        <label class="label"><span class="label-text">Início</span></label>
        <input name="start_date" type="datetime-local" class="input input-bordered" required>
      </div>
      <div class="form-control">
        <label class="label"><span class="label-text">Fim</span></label>
        <input name="end_date" type="datetime-local" class="input input-bordered" required>
      </div>
    </div>

    <div class="form-control">
      <label class="label"><span class="label-text">Tipo</span></label>
      <select name="type" class="select select-bordered w-full">
        <option value="radio">Seleção Única</option>
        <option value="checkbox">Múltipla Escolha</option>
      </select>
    </div>

    <label class="label cursor-pointer justify-start gap-4">
      <input type="checkbox" name="allow_blank" class="checkbox checkbox-primary" value="true">
      <span class="label-text">Permitir voto em branco</span>
    </label>

    <div class="form-control">
      <label class="label"><span class="label-text">Opções (uma por linha)</span></label>
      <textarea name="answers" class="textarea textarea-bordered h-24" required></textarea>
    </div>

    <button type="submit" class="btn btn-primary w-full mt-4">Publicar Enquete</button>
  </form>
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full mt-2">Cancelar</button>
</div>
{{end}}

{{define "polls"}}
<div class="space-y-4">
  <h2 class="text-2xl font-bold">Enquetes Administradas</h2>
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}
  {{if .Message}}<div class="alert alert-success">{{.Message}}</div>{{end}}
  <ul class="space-y-2">
    {{range .Polls}}
    <li class="flex gap-2">
       <button hx-get="/ui/polls/{{.ID}}/results" hx-target="#app" class="btn btn-outline flex-1 text-left justify-between">
         <span>{{.Title}}</span>
         <span class="text-xs font-normal text-gray-400">Ver Resultados</span>
       </button>
    </li>
    {{else}}
    <p class="text-gray-500">Nenhuma enquete encontrada.</p>
    {{end}}
  </ul>
  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full mt-4">← Painel Administrativo</button>
</div>
{{end}}

{{define "verify_form"}}
<div class="card bg-base-100 shadow-xl p-8">
  <h2 class="text-2xl font-bold mb-6">🔑 Verificar Acesso</h2>
  <form hx-post="/ui/verify" hx-target="#app" hx-swap="innerHTML" class="space-y-4">
    <input name="cpf" placeholder="CPF" class="input input-bordered w-full" required>
    <input name="passcode" placeholder="Código de 4 dígitos" class="input input-bordered w-full" required>
    <button class="btn btn-secondary w-full">Entrar</button>
  </form>
  <button hx-get="/ui/voting-flow" hx-target="#app" class="btn btn-ghost w-full mt-4">← Voltar</button>
</div>
{{end}}

{{define "admin_login"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-md mx-auto space-y-6">
  <h2 class="text-2xl font-bold text-center text-secondary">🔐 Login Administrador</h2>
  {{if .Error}}<div class="alert alert-error">{{.Error}}</div>{{end}}

  <form hx-post="/ui/admin/request-otp" hx-target="#app" class="bg-base-200 p-4 rounded-xl space-y-2">
    <span class="text-sm font-semibold text-gray-500 block">Usuários Normais: Solicite senha dinâmica via WhatsApp</span>
    <input name="username" placeholder="Seu CPF de Admin" class="input input-bordered w-full" required>
    <button class="btn btn-sm btn-outline btn-secondary w-full">Receber Senha via WhatsApp</button>
  </form>

  <div class="divider">ENTRAR</div>

  <form hx-post="/ui/admin/login" hx-target="#app" class="space-y-4">
    <input name="username" placeholder="Usuário (admin ou seu CPF)" class="input input-bordered w-full" required>
    <input name="password" type="password" placeholder="Senha (Fixa p/ SuperAdmin, Dinâmica p/ Normais)" class="input input-bordered w-full" required>
    <button class="btn btn-primary w-full">Entrar</button>
  </form>
  <button hx-get="/" hx-target="#app" class="btn btn-ghost w-full">← Voltar</button>
</div>
{{end}}

{{define "admin_change_password"}}
<div class="card bg-base-100 shadow-xl p-8 max-w-md mx-auto">
  <h2 class="text-2xl font-bold mb-6 text-center text-warning">🔄 Troca de Senha Obrigatória</h2>
  <form hx-post="/ui/admin/change-password" hx-target="#app" class="space-y-4">
    <input name="old_password" type="password" placeholder="Senha atual" class="input input-bordered w-full" required>
    <input name="new_password" type="password" placeholder="Nova senha (mín. 8 caracteres)" class="input input-bordered w-full" required>
    <button class="btn btn-primary w-full">Alterar Senha</button>
  </form>
</div>
{{end}}

{{define "poll_stats"}}
<div class="card bg-base-100 shadow-xl p-6">
  <h2 class="text-2xl font-bold mb-4">Estatísticas: {{.PollTitle}}</h2>
  <div class="stats shadow mb-4">
    <div class="stat"><div class="stat-title">Total de Votos</div><div class="stat-value">{{.TotalVotes}}</div></div>
  </div>

  <canvas id="pollChart"></canvas>
</div>

<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
<script>
  fetch('/ui/polls/stats/{{.PollID}}')
    .then(r => r.json())
    .then(data => {
      new Chart(document.getElementById('pollChart'), {
        type: 'pie',
        data: {
          labels: data.labels,
          datasets: [{ data: data.values, backgroundColor: ['#36A2EB', '#FF6384', '#FFCE56', '#4BC0C0'] }]
        }
      });
    });
</script>
{{end}}

{{define "global_stats"}}
<div class="card bg-base-100 shadow-xl p-6 space-y-6">
  <h2 class="text-2xl font-bold">📈 Estatísticas Globais</h2>

  <div class="stats shadow" id="globalStatsSummary">
    <div class="stat">
      <div class="stat-title">Total de Votos</div>
      <div class="stat-value" id="globalTotalVotes">—</div>
    </div>
    <div class="stat">
      <div class="stat-title">Comparecimento</div>
      <div class="stat-value" id="globalTurnout">—</div>
    </div>
  </div>

  <div>
    <h3 class="font-semibold mb-2">Votos por hora</h3>
    <canvas id="globalTimelineChart"></canvas>
    <p id="globalTimelineEmpty" class="text-sm opacity-60 hidden">Ainda não há votos registrados.</p>
  </div>

  <button hx-get="/ui/admin" hx-target="#app" class="btn btn-ghost w-full">← Painel Administrativo</button>
</div>

<script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
<script>
  fetch('/admin/stats')
    .then(r => r.json())
    .then(data => {
      document.getElementById('globalTotalVotes').textContent = data.total_votes ?? 0;
      document.getElementById('globalTurnout').textContent = (data.turnout_pct ?? 0).toFixed(1) + '%';

      const timeline = data.timeline || [];
      if (timeline.length === 0) {
        document.getElementById('globalTimelineEmpty').classList.remove('hidden');
        return;
      }

      new Chart(document.getElementById('globalTimelineChart'), {
        type: 'line',
        data: {
          labels: timeline.map(t => t.hour),
          datasets: [{
            label: 'Votos por hora',
            data: timeline.map(t => t.count),
            borderColor: '#36A2EB',
            backgroundColor: 'rgba(54, 162, 235, 0.2)',
            tension: 0.2,
            fill: true
          }]
        }
      });
    })
    .catch(() => {
      document.getElementById('globalTimelineEmpty').classList.remove('hidden');
    });
</script>
{{end}}

`))
