<div align="center">

# 📞 WaCalls (Go)

**Chamadas de voz nativas do WhatsApp em Go puro, direto do navegador.**
Feito para mídia VoIP nativa, operação multi-conta (multi-sessão) e um cliente web moderno.

[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go&logoColor=white)](https://go.dev)
[![React](https://img.shields.io/badge/React-19-61DAFB?logo=react&logoColor=black)](https://react.dev)
[![whatsmeow](https://img.shields.io/badge/whatsmeow-VoIP-25D366?logo=whatsapp&logoColor=white)](https://github.com/tulir/whatsmeow)
[![pion](https://img.shields.io/badge/pion-WebRTC-FF6B6B)](https://github.com/pion/webrtc)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](#licença)

[Visão geral](#visão-geral) · [Arquitetura](#arquitetura) · [Videochamada](#videochamada-experimental) · [Início rápido](#início-rápido) · [API](#api) · [Segurança](#segurança)

</div>

---

## Visão geral

O WaCalls pareia uma ou mais contas do WhatsApp via **QR code** e permite **fazer e
receber chamadas 1:1** de qualquer navegador na LAN. O microfone do navegador é enviado
como **PCM cru de 16 kHz por um data channel WebRTC** para o servidor Go, que codifica
com o codec **MLow** da Meta e injeta a mídia na malha de **relay SRTP** do WhatsApp — e
o caminho inverso traz o áudio do outro lado de volta ao navegador.

Toda a stack VoIP roda **nativamente em Go puro**: o codec de voz MLow, a packetização
**RTP/SRTP**, o **STUN**, o transporte **WebRTC/SCTP relay** e a sinalização `<call>`,
integrados com o [**whatsmeow**](https://github.com/tulir/whatsmeow) e servidos a um
cliente **React 19**. **Sem cgo e sem DLL nativa** — o codec MLow é um pacote Go puro
vendorizado, então um `go build` simples produz um binário self-contained com áudio ao
vivo.

Várias contas do WhatsApp podem ser pareadas e operadas lado a lado, cada uma com o
próprio QR de pareamento, status de conexão e histórico. Uma mesma conta também pode
rodar **várias chamadas 1:1 simultâneas** — uma por operador de navegador — roteadas de
forma independente pelo call ID.

**Videochamada** funciona de ponta a ponta no sentido de **entrada** (o vídeo H264 do
outro lado é decodificado e mostrado no navegador); veja
[Videochamada](#videochamada-experimental) para o estado atual do sentido de saída.

> **Estado:** a voz é estável — chamadas 1:1 de saída e de entrada chegam a `ACTIVE` com
> áudio bidirecional, e uma conta segura várias simultâneas. A entrada de vídeo e a
> estabilidade da chamada funcionam; o vídeo de saída (navegador → outro lado) ainda não
> é renderizado pelo WhatsApp — veja [Videochamada](#videochamada-experimental). As
> sessões persistem em `wacalls.db` (SQLite Go puro).

---

## Arquitetura

```
┌──────────────────────────────────────────────────────────────────────────┐
│                          NAVEGADOR (cliente React)                         │
│   mic + alto-falante  ·  data channel WebRTC (PCM 16 kHz)  ·  HTTP + SSE    │
└───────────────────────────────┬──────────────────────────────────────────┘
                                 │  POST /api/sessions/{sid}/calls/{id}/webrtc  (SDP)
                                 │  GET  /api/events                            (SSE)
                                 ▼
┌──────────────────────────── SERVIDOR GO (cmd/server) ──────────────────────┐
│  SessionManager   registro de contas (client + CallManager + bridge)       │
│  Broker           hub de SSE (sessões, auth, ciclo de vida da chamada)      │
│  Bridge           ponte WebRTC pion (data channel PCM 16 kHz ⇄ núcleo)      │
│                                                                            │
│  internal/wa      adaptador VoipSocket sobre o whatsmeow                   │
│  internal/voip    call · signaling · media · transport · core · wanode     │
└───────────────┬──────────────────────────────────────┬────────────────────┘
                │ sinalização <call> (Signal/USync)     │ mídia SRTP
                ▼                                        ▼
        ┌───────────────┐                    ┌──────────────────────┐
        │  WhatsApp WS  │                    │   relay do WhatsApp   │
        │  (whatsmeow)  │                    │  (SRTP sobre SCTP/DC) │
        └───────────────┘                    └──────────────────────┘
```

### Layout

| Caminho | Responsabilidade |
|---|---|
| `cmd/server` | broker HTTP/SSE, gerenciador de sessões + store, ponte WebRTC, ciclo de vida do processo |
| `internal/wa` | `VoipSocket` — envia/recebe stanzas `<call>` via whatsmeow |
| `internal/voip/core` | tipos de domínio, constantes, a interface `VoipSocket` |
| `internal/voip/wanode` | helpers compartilhados de nó do WhatsApp e JID |
| `internal/voip/media` | codec MLow (`mlow/` Go puro vendorizado), RTP, SRTP, SSRC, helpers de PCM, derivação de chave |
| `internal/voip/transport` | relay SCTP, STUN, codificação de subscription |
| `internal/voip/signaling` | build/parse de stanza `<call>`, cripto de call-key, parse do relay-ack |
| `internal/voip/call` | `CallManager` — orquestra uma chamada de ponta a ponta |
| `client/` | React 19 + Vite + Tailwind v4 + shadcn/ui (dialer, cards de chamada, sessões, histórico) |

---

## Como uma chamada flui

O núcleo é o `internal/voip/call.CallManager`, que conduz a chamada de ponta a ponta.
Sequência de uma chamada de saída:

```
1. POST .../calls            → CallManager.StartCall(peerJid)
                               gera um callID, monta o <call> offer, envia

2. Navegador abre o WebRTC   → POST .../calls/{id}/webrtc (SDP offer)
                               a ponte responde com um SDP answer (pion)

3. Outro lado aceita         → events.CallAccept → HandleCallAccept
                               servidor recebe <relay> + chaves hop-by-hop

4. Transporte de relay       → binding/allocate STUN nos relays do WhatsApp
                               ICE + DTLS + SCTP DataChannel conectam (pion)

5. Mídia SRTP fluindo        → estado vai para ACTIVE
   ├── subida  (você → peer): PCM 16 kHz do navegador (data channel) → MLow encode → SRTP → relay
   └── descida (peer → você): relay → SRTP → MLow decode → PCM 16 kHz (data channel) → navegador

6. Encerramento             → DELETE .../calls/{id} ou events.CallTerminate
                               CallManager.EndCall + limpeza da ponte
```

Cada passo de protocolo (derivação de chave SRTP hop-by-hop, packetização RTP em
`PT=120`/16 kHz, registro STUN no relay, parse do relay-ack e das stanzas `<call>`) está
implementado e coberto por testes em `internal/voip` (`go test ./...`).

---

## Videochamada (experimental)

Uma videochamada adiciona um segundo **data channel `vp8`** entre o navegador e o
servidor Go (o rótulo é histórico — o payload é **H264**). O navegador é dono do codec
via **WebCodecs** (`VideoEncoder`/`VideoDecoder`, Annex-B, `avc1.42E01F`); o lado Go só
empacota cada unidade de acesso em RTP (`PT=97`, FU-A/STAP-A,
`internal/voip/media/rtph264.go`) e devolve os quadros de entrada para exibição.

- **Sinalização** (`internal/voip/signaling`): o `<offer>`/`<accept>` levam
  `<video enc="h.264" dec="H264">` mais os nós `<capability>`, `<voip_settings>` e
  `<uploadfieldstat/>` que um cliente WhatsApp Business real envia.
- **Extensão de cabeçalho RTP**: todo pacote de vídeo carrega o bloco `0xDEBE` de 4
  elementos que um cliente real usa (orientação CVO + um contador de sequência
  transport-wide).
- **RTCP / estimativa de banda** (`internal/voip/call/callmanager_rtcp.go`,
  `internal/voip/media/{rtcp,srtcp}.go`): um compound SR/RR/REMB é enviado por SRTCP uma
  vez por segundo. Sem ele o WhatsApp derruba a videochamada em ~7 s; com ele a chamada
  fica estável.

**Funciona:** vídeo de entrada (peer → navegador), decodificado e mostrado na orientação
certa; estabilidade da chamada.

**Ainda não funciona:** o vídeo de saída (navegador → peer) é empacotado e chega ao
WhatsApp (ele manda NACK dos nossos pacotes), mas o outro lado não renderiza. As duas
peças que faltam são a retransmissão dos pacotes pedidos via NACK (RTX) e decodificar
100% o RTCP de entrada não-padrão ("fast") do WhatsApp. Rode o servidor com
`-video-dump` para logar o RTP/RTCP dos dois sentidos e comparar.

---

## Requisitos

- **Go 1.26+**
- **Node 22+** e **npm** (só para buildar/rodar o cliente React)

Não precisa de compilador C, cgo ou bibliotecas nativas — o codec MLow é Go puro
vendorizado (`internal/voip/media/mlow`).

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

O áudio ao vivo funciona de imediato — o codec MLow é Go puro, então um build simples já
o inclui. Sem build tags, sem `CGO_ENABLED`, sem DLLs.

Abra `http://localhost:8080`, clique em **New session** e escaneie o QR mostrado no
navegador (também impresso no terminal) em **WhatsApp → Aparelhos conectados**. Adicione
mais contas do mesmo jeito e alterne entre elas na barra lateral.

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
| `-debug` | `false` | logs verbosos (inclui o log interno do whatsmeow) |
| `-max-calls-per-session` | `8` | máx. de chamadas simultâneas por sessão (`0` = sem limite) |
| `-video-dump` | `false` | loga o RTP/RTCP de videochamada em detalhe (depuração do vídeo de saída) |

---

## API

Todas as rotas são por sessão. Os eventos chegam por um único canal SSE, marcados com o
`sessionId` de origem.

| Método | Rota | Propósito |
|---|---|---|
| `GET` | `/api/sessions` | lista as contas (id, name, jid, status, paired) |
| `POST` | `/api/sessions` | cria uma conta e começa o pareamento por QR |
| `DELETE` | `/api/sessions/{sid}` | desloga e remove uma conta |
| `POST` | `/api/sessions/{sid}/logout` | desconecta uma conta (mantém para re-parear) |
| `POST` | `/api/sessions/{sid}/pair` | re-pareia uma conta (emite um QR novo) |
| `POST` | `/api/sessions/{sid}/calls` | inicia uma chamada de saída (`{ phone, duration_ms?, record?, video? }`) |
| `POST` | `/api/sessions/{sid}/calls/{id}/webrtc` | troca o SDP WebRTC do navegador (uma videochamada também abre um data channel `vp8`) |
| `POST` | `/api/sessions/{sid}/calls/{id}/accept` | atende uma chamada de entrada |
| `POST` | `/api/sessions/{sid}/calls/{id}/reject` | rejeita uma chamada de entrada |
| `DELETE` | `/api/sessions/{sid}/calls/{id}` | encerra uma chamada ativa |
| `GET` | `/api/sessions/{sid}/history` | histórico recente de chamadas (até 50 registros) |
| `GET` | `/api/events` | server-sent events (sessões, auth, ciclo de vida da chamada) |

---

## Testes

```bash
go test ./...                 # stack de mídia: SRTP, STUN, RTP, relay-ack, codec, state
cd client && npm run build    # type-check + build de produção do cliente
```

---

## Segurança

A API **não tem autenticação** — qualquer um com acesso HTTP pode criar contas, fazer
chamadas e ler o histórico. **Rode só numa LAN confiável.** O `wacalls.db` guarda
credenciais de sessão do WhatsApp (segredos): **não commite** e mantenha protegido.

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

- [**whatsmeow**](https://github.com/tulir/whatsmeow) — biblioteca Go do protocolo WhatsApp Web
- [**pion/webrtc**](https://github.com/pion/webrtc) — stack WebRTC Go puro (ICE + DTLS + SCTP)
- [**whatsapp-rust**](https://github.com/oxidezap/whatsapp-rust) — implementação de referência do codec MLow (portada para o `internal/voip/media/mlow` Go puro vendorizado)
- [**zapo**](https://github.com/w3nder/zapo) — referência de stack de mídia VoIP

---

## Licença

[MIT](./LICENSE)
