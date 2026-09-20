<div align="center">

# 📞 WaCalls (Go)

**Chamadas de voz nativas do WhatsApp em Go puro, direto do navegador.**
Feito para mídia VoIP nativa, operação multi-conta (multi-sessão) e um cliente web moderno.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![meowcaller](https://img.shields.io/badge/meowcaller-VoIP-25D366?logo=whatsapp&logoColor=white)](https://github.com/purpshell/meowcaller)
[![pion](https://img.shields.io/badge/pion-WebRTC-FF6B6B)](https://github.com/pion/webrtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#licença)

[Visão geral](#visão-geral) · [Arquitetura](#arquitetura) · [Videochamada](#videochamada) · [Recursos de chamada](#recursos-de-chamada) · [Início rápido](#início-rápido) · [API](#api) · [Segurança](#segurança)

</div>

---

## Visão geral

O WaCalls pareia uma ou mais contas do WhatsApp via **QR code** e permite **fazer e
receber chamadas de voz e vídeo** de qualquer navegador na LAN — 1:1, em grupo, com
reações, mão levantada, hold/transferência e handoff entre agentes. O microfone (e,
numa chamada de vídeo, a câmera) do navegador é enviado por **data channels WebRTC**
para o servidor Go, que repassa a mídia pro [**meowcaller**](https://github.com/purpshell/meowcaller)
— uma biblioteca de chamadas VoIP construída sobre o [**HyperMeow**](https://github.com/polymorfa/hypermeow)
(um fork do protocolo WhatsApp Web) — e o caminho inverso traz áudio/vídeo do outro lado
de volta ao navegador.

O servidor Go fica fino: `cmd/server` cuida do broker HTTP/SSE, do gerenciamento de
sessões e da ponte WebRTC (pion) entre o navegador e a `Call` do meowcaller — toda a
sinalização `<call>`, codec, SRTP/RTP e negociação de relay do WhatsApp ficam dentro do
meowcaller/HyperMeow como dependência Go. Cliente **React 19**.

Várias contas do WhatsApp podem ser pareadas e operadas lado a lado, cada uma com o
próprio QR de pareamento, status de conexão e histórico. Uma mesma conta também pode
rodar **várias chamadas simultâneas** — uma por operador de navegador — roteadas de
forma independente pelo call ID, com suporte a hold, transferência cega e handoff de
bridge entre agentes (ver [Recursos de chamada](#recursos-de-chamada)).

**Videochamada** funciona de ponta a ponta nos dois sentidos — entrada e saída — desde
que o quadro repassado ao navegador carregue um `Keyframe`/`TimestampMS` corretos
(ver [Videochamada](#videochamada)).

> **Estado:** voz e vídeo são estáveis nos dois sentidos, 1:1 e em grupo. Reações
> (emoji ou texto livre), mão levantada, hold/unhold, transferência cega com fallback
> automático e handoff de chamada entre agentes (pickup) estão implementados —
> ver [Recursos de chamada](#recursos-de-chamada) pro que já foi validado ao vivo vs.
> só revisado em código. As sessões persistem em `wacalls.db` (SQLite Go puro).

---

## Arquitetura

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          NAVEGADOR (cliente React)                         │
│  mic + câmera + alto-falante  ·  data channels WebRTC (PCM/H264)  ·  SSE   │
└───────────────────────────────┬──────────────────────────────────────────┘
                                 │  POST /api/sessions/{sid}/calls/{id}/webrtc  (SDP)
                                 │  GET  /api/events                            (SSE)
                                 ▼
┌──────────────────────────── SERVIDOR GO (cmd/server) ──────────────────────┐
│  SessionManager   registro de contas (client whatsmeow/hypermeow + meow)   │
│  Broker           hub de SSE (sessões, auth, ciclo de vida da chamada)      │
│  Bridge           ponte WebRTC pion (data channels ⇄ meowcaller.Call)       │
│  callRegistry     chamadas ativas por sessão (Call + AudioSource + bridge) │
└───────────────────────────────┬──────────────────────────────────────────┘
                                 │ meowcaller.Client (Call/AddParticipant/
                                 │ SendReaction/SetHandRaised/Hold/Transfer…)
                                 ▼
                    ┌──────────────────────────────┐
                    │   meowcaller (sobre HyperMeow) │
                    │  sinalização <call>, SRTP/RTP,  │
                    │  relay SCTP, codec — tudo como  │
                    │  dependência Go                 │
                    └──────────────────────────────┘
```

`internal/voip/*` era o motor de chamada feito à mão (RTP/SRTP/MLow/sinalização sobre
whatsmeow) que o WaCalls usava antes de migrar pro meowcaller. O código ainda está no
repo e passa em `go test ./...`, mas **não está mais no caminho servido** por
`cmd/server` — ver `.ai/known-issues.md` pro histórico da migração.

### Layout

| Caminho | Responsabilidade |
|---|---|
| `cmd/server` | broker HTTP/SSE, gerenciador de sessões, ponte WebRTC, registro de chamadas ativas, ciclo de vida do processo |
| `client/` | React 19 + Vite + Tailwind v4 + shadcn/ui (dialer, cards de chamada, sessões, histórico) |
| `client/public/api-docs.html` + `openapi.yaml` | documentação interativa da API (Swagger UI) — ver [API](#api) |
| `internal/voip/*` | motor de chamada legado (RTP/SRTP/MLow feito à mão) — fora do caminho servido, mantido só como referência histórica |

---

## Como uma chamada flui

Sequência de uma chamada de saída:

```
1. POST .../calls             → session.startOutgoing(peer) → meow.CallWithOptions
                                gera um callID, meowcaller manda o <call> offer

2. Navegador abre o WebRTC    → POST .../calls/{id}/webrtc (SDP offer)
                                Bridge (pion) responde com um SDP answer

3. Outro lado aceita          → meowcaller entrega o relay + inicia a mídia
                                Call.Play(browser mic) / Call.Receive(→ bridge)

4. Mídia fluindo              → estado vai para "connected"
   ├── subida  (você → peer): PCM do navegador (data channel) → liveAudioSource → meowcaller → relay
   └── descida (peer → você): relay → meowcaller → bridge.WritePCM → data channel → navegador
   (numa chamada de vídeo, o mesmo par de caminhos existe pro data channel "vp8",
   com Keyframe/TimestampMS carimbados em cada quadro repassado — ver Videochamada)

5. Encerramento               → DELETE .../calls/{id} ou Call.OnEnd (peer desligou)
                                sess.removeCall + limpeza da ponte
```

Todo o protocolo (sinalização `<call>`, SRTP/RTP, negociação de relay, codec) fica
dentro do meowcaller/HyperMeow como dependência — o WaCalls só liga os data channels
WebRTC do navegador nos pontos `Call.Play`/`Call.Receive`/`Call.SendVideo`/`Call.ReceiveVideo`.

---

## Videochamada

Uma videochamada adiciona um segundo **data channel `vp8`** entre o navegador e o
servidor Go (o rótulo é histórico — o payload é **H264**). O navegador é dono do codec
via **WebCodecs** (`VideoEncoder`/`VideoDecoder`, Annex-B, `avc1.42E01F`,
`client/src/lib/video-pipe.ts`); o lado Go só repassa cada access unit entre o data
channel e `Call.SendVideo`/`Call.ReceiveVideo` do meowcaller.

**Funciona nos dois sentidos** — entrada (peer → navegador) e saída (navegador → peer),
decodificado e mostrado na orientação certa.

O vídeo de saída não renderizava no início da migração pro meowcaller, mesmo com o
áudio funcionando: o decoder WebCodecs do navegador só arranca a partir de um quadro
marcado como `keyframe`, com um timestamp real e crescente — e o código que repassava
os quadros do meowcaller pro bridge (`cmd/server/httpapi.go`, `doWebRTC`) nunca setava
`Keyframe`/`TimestampMS` em `media.VideoFrame`. Todo quadro chegava e era descartado em
silêncio pelo navegador, pra sempre. Fix: detectar IDR com `rtp.AUHasIDR` (exportado
pelo meowcaller) e carimbar `TimestampMS` com o tempo decorrido desde o início do vídeo
na chamada. Histórico completo da investigação em `.ai/known-issues.md`.

---

## Recursos de chamada

Além de chamada 1:1 de voz/vídeo, o servidor expõe (via `meowcaller.Client`/`Call`):

| Recurso | Rota | Notas |
|---|---|---|
| Reação (emoji ou texto livre) | `POST .../calls/{id}/reaction` | vai pelo canal de app-data da própria chamada, não é mensagem de chat |
| Levantar/abaixar a mão | `POST .../calls/{id}/hand` | estado persistente do participante |
| Adicionar participante | `POST .../calls/{id}/participants` | promove uma chamada direta pra grupo ad-hoc |
| Chamada em grupo | `POST .../calls` com `{ group: "<jid>@g.us" }` | toca todo membro remoto do grupo (WhatsApp exige 2+ remotos) |
| Migrar áudio ↔ vídeo no meio da chamada | `POST .../webrtc/renegotiate` + `.../video/start` / `.../video/stop` | uma chamada que começou só de áudio ganha vídeo sem discar de novo — renegociação WebRTC de verdade na mesma peer connection, mais `Call.StartVideo`/`StopVideo` do meowcaller pra sinalizar o upgrade pro WhatsApp. Confirmado ao vivo (vídeo visível de verdade no celular do peer) |
| Aceitar upgrade de vídeo pedido pelo peer | `POST .../video/accept` | nunca automático — a UI mostra uma notificação ("Aceitar") quando o peer pede pra virar vídeo do lado dele; `Call.AcceptVideo()` + `Call.StartVideo()` |
| Hold / unhold | `PUT .../calls/{id}/hold` / `.../unhold` | toca um tom local de 440 Hz; `moh_url` é aceito no corpo mas não decodificado (ver `.ai/known-issues.md`) |
| Transferência cega | `POST .../calls/{id}/transfer` | hold automático + nova chamada sainte em outra sessão; aceite encerra a original, rejeição/timeout de 60s restaura o hold |
| Pickup (handoff entre agentes) | `POST .../calls/{id}/pickup` | fecha o bridge WebRTC atual e reatribui a chamada a quem chamar o endpoint, incondicionalmente |

Compartilhamento de tela foi implementado e removido: o nó de sinalização `screen_share`
do meowcaller só é válido dentro de uma chamada de grupo de verdade no protocolo do
WhatsApp — numa chamada direta o servidor do WhatsApp rejeita e derruba a chamada
inteira. Detalhes em `.ai/known-issues.md`.

---

## Requisitos

- **Go 1.26+**
- **Node 22+** e **npm** (só para buildar/rodar o cliente React)

Não precisa de compilador C, cgo ou bibliotecas nativas — meowcaller, HyperMeow e o
resto da stack são Go puro (`CGO_ENABLED=0 go build ./...` funciona).

---

## Início rápido

```bash
# clonar e entrar no projeto
git clone <repo-url> wacalls-go
cd wacalls-go

# dependências Go
go mod download

# dependências do cliente React
cd client && npm install && cd ..
```

### Rodar

```bash
go run ./cmd/server -addr :8080          # adicione -debug para logs verbosos
```

Áudio e vídeo ao vivo funcionam de imediato — sem build tags, sem `CGO_ENABLED`, sem
DLLs. Abra `http://localhost:8080`, clique em **New session** e escaneie o QR mostrado
no navegador (também impresso no terminal) em **WhatsApp → Aparelhos conectados**.
Adicione mais contas do mesmo jeito e alterne entre elas na barra lateral — o WhatsApp
permite até 4 dispositivos vinculados na mesma conta, então dá pra testar recursos
multiagente (pickup, transferência) pareando duas sessões no mesmo número.

### Cliente React em modo dev

```bash
cd client
npm run dev      # Vite em :5173, faz proxy de /api → http://localhost:8080
```

Para produção, builde o cliente estático e sirva pelo servidor Go:

```bash
cd client && npm run build && cd ..
go run ./cmd/server -static client/dist -addr :8080
```

### Flags do servidor

| Flag | Padrão | Significado |
|---|---|---|
| `-addr` | `:8080` | endereço HTTP de escuta |
| `-db` | `wacalls.db` | caminho do banco SQLite de sessões |
| `-static` | `client/dist` | diretório do cliente estático (opcional) |
| `-debug` | `false` | logs verbosos (inclui o log de chamada/mídia do meowcaller e do hypermeow) |
| `-max-calls-per-session` | `8` | máx. de chamadas simultâneas por sessão (`0` = sem limite) |
| `-video-dump` | `false` | flag legada do motor `internal/voip/call` (fora do caminho servido); sem efeito na stack atual |

---

## API

Documentação interativa (Swagger UI) em **`/api-docs.html`** — com "Try it out" que
funciona de verdade contra o servidor rodando. `openapi.yaml` fica em `client/public/`,
servido estático junto com o cliente.

Todas as rotas são por sessão e aceitam o header opcional `X-Client-Id` (dono da
chamada / filtro de eventos), mais `X-API-Key` se `WACALLS_API_KEY` estiver setado no
ambiente. Os eventos chegam por um único canal SSE, marcados com o `sessionId` de
origem.

| Método | Rota | Propósito |
|---|---|---|
| `GET` | `/api/config` | limite de chamadas simultâneas configurado no servidor |
| `GET` | `/api/sessions` | lista as contas (id, name, jid, status, paired) |
| `POST` | `/api/sessions` | cria uma conta e começa o pareamento por QR |
| `DELETE` | `/api/sessions/{sid}` | desloga e remove uma conta |
| `POST` | `/api/sessions/{sid}/logout` | desconecta uma conta (mantém para re-parear) |
| `POST` | `/api/sessions/{sid}/pair` | re-pareia uma conta (emite um QR novo) |
| `GET` | `/api/sessions/{sid}/calls` | quantas chamadas ativas a sessão tem (e o limite) |
| `POST` | `/api/sessions/{sid}/calls` | inicia uma chamada (`{ phone \| group, video?, record? }`) |
| `GET` | `/api/sessions/{sid}/calls/{id}` | estado de uma chamada específica |
| `POST` | `/api/sessions/{sid}/calls/{id}/webrtc` | troca o SDP WebRTC do navegador (vídeo abre um data channel `vp8` também) |
| `POST` | `/api/sessions/{sid}/calls/{id}/webrtc/renegotiate` | renegocia a mesma peer connection (upgrade de áudio pra vídeo no meio da chamada) |
| `POST` | `/api/sessions/{sid}/calls/{id}/video/start` \| `/stop` | sinaliza upgrade/downgrade de vídeo pro WhatsApp |
| `POST` | `/api/sessions/{sid}/calls/{id}/accept` \| `/reject` | atende / rejeita uma chamada de entrada |
| `DELETE` | `/api/sessions/{sid}/calls/{id}` | encerra uma chamada ativa |
| `PUT` | `/api/sessions/{sid}/calls/{id}/hold` \| `/unhold` | põe em espera / retoma |
| `POST` | `/api/sessions/{sid}/calls/{id}/pickup` | handoff de bridge pra outro agente |
| `POST` | `/api/sessions/{sid}/calls/{id}/transfer` | transferência cega com fallback |
| `POST` | `/api/sessions/{sid}/calls/{id}/reaction` \| `/hand` \| `/participants` | reação, mão levantada, adicionar participante |
| `GET` | `/api/sessions/{sid}/history` | histórico recente de chamadas (até 50 registros) |
| `GET` | `/api/events` | server-sent events (sessões, auth, ciclo de vida da chamada, hold, pickup, transferência) |

---

## Testes

```bash
go test ./...                 # stack de mídia: SRTP, STUN, RTP, relay-ack, codec, state
cd client && npm run build    # type-check + build de produção do cliente
```

---

## Segurança

Por padrão a API **não tem autenticação** — qualquer um com acesso HTTP pode criar
contas, fazer chamadas e ler o histórico. **Rode só numa LAN confiável**, ou defina
`WACALLS_API_KEY` no ambiente pra exigir o header `X-API-Key` em toda requisição.
O `wacalls.db` (e qualquer backup `*.db.*`) guarda credenciais de sessão do WhatsApp
(segredos): **não commite** e mantenha protegido — ambos já cobertos pelo `.gitignore`.

---

## Contribuidores

Este projeto é construído sobre o trabalho de:

<div align="center">

<a href="https://github.com/jotadev66"><img src="https://github.com/jotadev66.png" width="72" height="72" style="border-radius:50%" alt="jotadev66"/></a>
<a href="https://github.com/jobasfernandes"><img src="https://github.com/jobasfernandes.png" width="72" height="72" style="border-radius:50%" alt="jobasfernandes"/></a>
<a href="https://github.com/edgardmessias"><img src="https://github.com/edgardmessias.png" width="72" height="72" style="border-radius:50%" alt="edgardmessias"/></a>
<a href="https://github.com/w3nder"><img src="https://github.com/w3nder.png" width="72" height="72" style="border-radius:50%" alt="w3nder"/></a>
<a href="https://github.com/fabriciosprj"><img src="https://github.com/fabriciosprj.png" width="72" height="72" style="border-radius:50%" alt="fabriciosprj"/></a>

[**@jotadev66**](https://github.com/jotadev66) · [**@jobasfernandes**](https://github.com/jobasfernandes) · [**@edgardmessias**](https://github.com/edgardmessias) · [**@w3nder**](https://github.com/w3nder) · [**@fabriciosprj**](https://github.com/fabriciosprj) *(videochamada H264)*

</div>

---

## Agradecimentos

- [**meowcaller**](https://github.com/purpshell/meowcaller) — biblioteca de chamadas VoIP (voz, vídeo, grupo, reações, hold/transferência) sobre o HyperMeow
- [**HyperMeow**](https://github.com/polymorfa/hypermeow) — fork do [whatsmeow](https://github.com/tulir/whatsmeow), biblioteca Go do protocolo WhatsApp Web
- [**pion/webrtc**](https://github.com/pion/webrtc) — stack WebRTC Go puro (ICE + DTLS + SCTP)
- [**whatsapp-rust**](https://github.com/oxidezap/whatsapp-rust) — implementação de referência do codec MLow (usado pelo motor legado em `internal/voip/media/mlow`)
- [**zapo**](https://github.com/w3nder/zapo) — referência de stack de mídia VoIP

---

## Licença

[MIT](./LICENSE)
