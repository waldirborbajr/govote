# CI/CD govote → Raspberry Pi via Tailscale

Documentação do pipeline de deploy automático do `govote` (GitHub Actions → GHCR → Raspberry Pi), incluindo os pontos que realmente causaram falha e como evitá-los numa configuração nova.

## Visão geral do fluxo

```
push/dispatch na main
  → test (go test, go vet)
  → build-and-push (build multi-plataforma arm64 + push GHCR)
  → deploy (conecta no tailnet → SSH no Pi → docker compose up)
```

---

## 1. Tailscale — OAuth Client

O runner efêmero do GitHub Actions entra no tailnet via **OAuth client**, não via auth key manual.

**Criar em:** `Trust credentials` (admin console) → `+ Credential` → OAuth → Scopes:
- **Auth Keys**: Read + Write
- **Tags**: todas as tags que o runner vai advertisar (ver seção 3)

⚠️ **Regra crítica descoberta na prática:** o valor do input `tags:` no step do GitHub Action precisa bater **exatamente** com o conjunto de tags autorizado no client. Pedir um subconjunto (ex: client autorizado para `tag:ci,tag:rasp` mas o workflow pedindo só `tag:ci`) é **rejeitado** pela API com `requested tags [...] are invalid or not permitted`, mesmo a tag em si sendo válida.

O Client Secret só é exibido **uma vez**, na criação. Não existe "regenerar secret" para um client existente — só `Edit` (metadados/escopos) ou `Revoke`. Se perder o secret, revoga e cria um novo client.

**Secrets no GitHub** (`Settings → Secrets and variables → Actions`):
| Secret | Valor |
|---|---|
| `TS_OAUTH_CLIENT_ID` | Client ID do OAuth client |
| `TS_OAUTH_SECRET` | Client Secret (copiado na criação) |

---

## 2. Tailscale — ACL Policy

Duas seções da policy precisam de ajuste: `tagOwners` e `ssh`.

### tagOwners
```json
"tagOwners": {
  "tag:rasp": ["autogroup:admin"],
  "tag:ci":   ["autogroup:admin"],
},
```

### ssh
Por padrão, o bloco `ssh` só libera humanos (`autogroup:member`) a acessar seus **próprios** dispositivos (`autogroup:self`). Isso não cobre nem o runner de CI nem — depois que o Pi vira tagged — o próprio dono acessando via SSH Console web.

```json
"ssh": [
  // Acesso padrão: membros aos próprios dispositivos não-tagged
  {
    "action": "check",
    "src":    ["autogroup:member"],
    "dst":    ["autogroup:self"],
    "users":  ["autogroup:nonroot", "root"],
  },
  // CI (runner efêmero) → Pi
  {
    "action": "accept",
    "src":    ["tag:ci"],
    "dst":    ["tag:rasp"],
    "users":  ["borba"],
  },
  // Dono acessando o Pi tagged (ex: via SSH Console do navegador)
  {
    "action": "check",
    "src":    ["autogroup:member"],
    "dst":    ["tag:rasp"],
    "users":  ["autogroup:nonroot", "root"],
  },
],
```

⚠️ **Ponto que causou o erro mais difícil de diagnosticar:** o Tailscale SSH intercepta a conexão SSH *antes* dela chegar no `sshd` do host. Quando a política nega, o cliente recebe apenas um `ssh: handshake failed: EOF` genérico — sem nenhuma mensagem clara — a menos que se rode `ssh -vvv` manual, que revela a linha real:
```
tailscale: tailnet policy does not permit you to SSH to this node
```
Se aparecer esse `EOF` sem explicação, suspeitar da política do Tailscale SSH antes de qualquer outra coisa (firewall, sshd_config, algoritmos, etc.).

---

## 3. Raspberry Pi — configuração do Tailscale

O Pi precisa advertisar a tag `tag:rasp` (usada como `dst` na regra de ACL acima):

```bash
sudo tailscale up --ssh --advertise-exit-node --advertise-tags=tag:rasp
```

⚠️ **Atenção:** assim que um dispositivo recebe uma tag, ele **deixa de pertencer ao `autogroup:self`** do usuário dono — mesmo sendo fisicamente o mesmo Pi de sempre. Isso quebra o acesso via SSH Console web até a terceira regra de `ssh` (seção 2) ser adicionada.

Ao alterar tags com `tailscale up`, se o comando reclamar de flags implícitas:
```
Error: changing settings via 'tailscale up' requires mentioning all non-default flags...
```
é preciso repetir **todas** as flags não-default já ativas (ex: `--ssh`, `--advertise-exit-node`) junto com a nova, senão elas são resetadas.

---

## 4. Raspberry Pi — acesso SSH para o deploy

O runner do GitHub Actions loga como usuário `borba` via `appleboy/ssh-action`, autenticando por chave.

**No Pi, usuário `borba`:**
```bash
cat ~/.ssh/id_ed25519.pub >> ~/.ssh/authorized_keys
chmod 700 ~/.ssh
chmod 600 ~/.ssh/authorized_keys
```

**Secrets no GitHub:**
| Secret | Valor |
|---|---|
| `SSH_PRIVATE_KEY` | Conteúdo completo da chave **privada** (`cat ~/.ssh/id_ed25519`), incluindo `-----BEGIN/END-----` |
| `VPS_HOST` | IP Tailscale do Pi (ex: `100.74.128.100`) |
| `VPS_USER` | `borba` |

⚠️ Sem `authorized_keys` populado, a tentativa cai automaticamente para autenticação por senha (`ssh: no key found` no log do lado cliente antes disso; depois vira prompt de senha).

**Grupo docker:** o usuário usado no deploy precisa rodar `docker`/`docker compose` sem sudo:
```bash
sudo usermod -aG docker borba
```
(requer novo login/sessão para valer)

---

## 5. Workflow — pontos de atenção no YAML

### `workflow_dispatch` não é `push`
Os jobs `build-and-push` e `deploy` tinham `if: github.event_name == 'push'`, o que bloqueia disparo manual (usado bastante durante o troubleshooting). Corrigido para:
```yaml
if: github.ref_name == 'main' && (github.event_name == 'push' || github.event_name == 'workflow_dispatch')
```

### Tags do step Tailscale
```yaml
- name: Conectar ao tailnet
  uses: tailscale/github-action@v4
  with:
    oauth-client-id: ${{ secrets.TS_OAUTH_CLIENT_ID }}
    oauth-secret: ${{ secrets.TS_OAUTH_SECRET }}
    tags: tag:ci,tag:rasp   # precisa bater com o escopo do OAuth client
```

---

## 6. Checklist de verificação rápida (se o deploy voltar a falhar)

1. `sudo tailscale status` no Pi → confirma que `rasplab` está com `tag:rasp` (não `wborbajr@`)
2. Testar troca de client_secret → token via `curl` na API do Tailscale, e criar uma auth key de teste pedindo o **mesmo conjunto de tags** do client — isola se o problema é a policy/client ou os secrets do GitHub
3. Rodar `ssh -vvv` manual (de dentro de um step de debug no workflow, ou local) contra o `VPS_HOST` — revela a causa real muito mais rápido que interpretar `EOF` sozinho
4. Confirmar `authorized_keys` no usuário de deploy no Pi
5. Confirmar que o secret `SSH_PRIVATE_KEY` no GitHub é a chave **privada** completa, não a `.pub`
6. Rodar `sudo -n chown -R 65532:65532 /home/borba/ghdeploy/govote/data` manualmente no Pi como `borba` → deve executar **sem pedir senha**; se pedir, a regra do `zz-govote-deploy` (seção 8) não bateu com o comando esperado
7. Conferir a porta exposta (`docker compose ps` / `docker inspect`) contra a porta configurada no reverse proxy/serviço — mismatch de porta sobe o container "saudável" mas sem responder (seção 7.2)

---

## 7. Docker — imagem `scratch` non-root e mismatch de porta

Dois problemas apareceram só depois que o SSH/Tailscale já estavam funcionando — o container subia, mas falhava ao rodar ou não respondia.

### 7.1 Permissão de filesystem — usuário non-root em imagem `scratch`

A imagem final do `govote` usa `FROM scratch` (multi-stage build), rodando como usuário não-root `65532:65532` (convenção do `distroless`/`nonroot`, mesmo em `scratch`). Como não existe shell nem `chown` disponível dentro da imagem final, qualquer diretório montado via volume (ex: dados persistentes) precisa **já vir com a ownership correta do host**, ou o processo falha ao tentar escrever/ler com permissão negada.

**Fix aplicado:**
```dockerfile
# no estágio de build, antes de copiar pro scratch
RUN mkdir -p /data && chown -R 65532:65532 /data
...
FROM scratch
COPY --from=build --chown=65532:65532 /data /data
USER 65532:65532
```

E no host (`rasplab`), o diretório de bind mount usado no `docker-compose.yaml` precisa da mesma ownership. Como o usuário de deploy (`borba`) não tem mais `sudo` amplo (seção 8), a correção de ownership no host passa a ser feita via a regra restrita do `sudoers.d`:
```bash
sudo chown -R 65532:65532 /home/borba/ghdeploy/govote/data
```

### 7.2 Mismatch de porta HTTP (`8080` → `9080`)

A aplicação estava configurada (hardcoded ou via env padrão) para escutar em `8080`, mas o `docker-compose.yaml`/reverse proxy no Pi esperava `9080` — resultado: container subia saudável, mas nada respondia na porta esperada.

**Fix aplicado:** alinhar a porta em todos os pontos:
```yaml
# docker-compose.yaml
services:
  govote:
    ports:
      - "9080:9080"
    environment:
      - PORT=9080   # ou o nome da env var que o govote lê
```
Confirmado que o binário Go lê a porta da env var (não fica fixo em `8080` no código) antes de considerar o fix definitivo.

---

## 8. Raspberry Pi — hardening de sudo (`/etc/sudoers.d/zz-govote-deploy`)

Depois que o deploy via CI/CD ficou estável (seções 1–7), o `sudo` do usuário `borba` foi restrito. Em vez de manter `borba` no grupo `sudo`/`wheel` com acesso irrestrito, ele foi removido de lá e recebeu uma regra pontual via `visudo`, liberando **apenas** o comando `chown` necessário pelo fix da seção 7.1 (ownership do diretório de dados pro usuário non-root `65532:65532` da imagem `scratch`).

**Arquivo:** `/etc/sudoers.d/zz-govote-deploy`
(prefixo `zz` garante que seja processado por último, sobrepondo qualquer regra anterior pro mesmo usuário)

**Criado com:**
```bash
sudo visudo -f /etc/sudoers.d/zz-govote-deploy
```

**Conteúdo:**
```
borba ALL=(ALL) NOPASSWD: /usr/bin/chown -R 65532\:65532 /home/borba/ghdeploy/govote/data
```

Ou seja: `borba` pode rodar, sem senha, **somente** esse `chown` exato — com esse path e essa ownership específicos. Qualquer outro uso de `sudo` (inclusive `chown` com argumentos diferentes) continua bloqueado.

**Motivação:** o runner do GitHub Actions loga como `borba` via SSH pra fazer o deploy (seção 4). Antes desse ajuste, corrigir a ownership de `/home/borba/ghdeploy/govote/data` (seção 7.1) exigiria `sudo` genérico. Restringindo a regra a esse comando exato, reduz-se drasticamente o raio de dano caso a chave SSH usada pelo CI (`SSH_PRIVATE_KEY`) vaze — o máximo que um atacante consegue via `sudo` é rodar esse `chown` específico, não comandos arbitrários como root.

⚠️ **`visudo` valida a sintaxe automaticamente ao salvar** — se o arquivo tiver erro, ele avisa e não aplica, o que evita o risco de travar o `sudo` do sistema (diferente de editar o arquivo direto com outro editor).

⚠️ **Sensibilidade do comando no workflow:** o step de deploy que roda `sudo chown -R 65532:65532 ...` só passa sem senha se bater **exatamente** com a linha do sudoers (mesmo path, mesmo UID:GID, mesmo `-R`). Qualquer variação (caminho diferente, flags a mais) exige senha e quebra o deploy silenciosamente em modo não-interativo. Validar com:
```bash
sudo -n chown -R 65532:65532 /home/borba/ghdeploy/govote/data
```

---

## Débitos técnicos / pendências identificadas

- ~~Permissão de filesystem non-root em imagem `scratch`~~ — **resolvido** (seção 7.1)
- ~~Mismatch de porta HTTP `8080`/`9080`~~ — **resolvido** (seção 7.2)
- ~~`sudo` irrestrito para o usuário de deploy~~ — **resolvido** (seção 8, regra restrita via `zz-govote-deploy`)
- IP forwarding desabilitado no Pi (warning recorrente do `tailscale status`): só relevante se depender de subnet routing de verdade.
- Duplicidade `grants` + `acls` no mesmo arquivo de ACL policy — Tailscale recomenda usar só um dos dois formatos.
