... (full template with addition in admin_dashboard) ... 
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

    <button hx-get="/ui/admin" hx-target="#app" class="btn btn-info h-24 text-lg" hx-on::after-request="if(this.closest('.modal')) this.closest('.modal').close()">
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
... (keep rest of templates) ...