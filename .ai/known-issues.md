# Known issues

## [NÃO IMPLEMENTADO 2026-09-18] Import/export de sessão (Baileys creds.json / wa-web dump)

Ao criar `client/public/api-docs.html` + `openapi.yaml` usando
`https://wacalls.globalcontrolseg.com.br/api-docs.html` como base, o spec de
referência daquele deploy documentava rotas de import/export de sessão
(`POST .../import`, `.../importRaw`, `GET .../export`, `.../importStatus`)
que não existem neste branch. Implementei todo o resto que faltava (ver
entrada seguinte) — **menos isso**.

**Por quê ficou de fora:** compatibilidade real com o formato Baileys
`creds.json` (ou um dump wa-web) exigiria mapear campos totalmente
diferentes (`noiseKey`, `signedIdentityKey`, `signedPreKey`,
`registrationId`, `advSecretKey`, sessões libsignal, etc.) para o
`store.Device` do hypermeow — um trabalho de reverse-engineering de formato,
sem implementação de referência neste repo ou nas dependências, mexendo em
material de identidade/chave criptográfica. Errar isso silenciosamente
corrompe ou expõe mal uma sessão real. Não é algo pra fazer sem pesquisa e
teste dedicados contra exports Baileys reais.

Se precisar de verdade migrar credenciais de um bot baseado em
Baileys/wa-web pra cá, trate como uma tarefa separada e dedicada — não uma
extensão rápida disso aqui.

## [ADICIONADO 2026-09-18] Hold/unhold, pickup, transfer, /api/config, API key

Motivado pelo mesmo trabalho de docs acima ("o que não tem, cria"). Tudo
implementado e buildando; testado ao vivo o que dava pra testar com uma
única sessão pareada:

- **`GET /api/config`, `GET .../calls` (contagem), `GET .../calls/{id}`**:
  testados ao vivo, ok.
- **`PUT .../hold` / `.../unhold`**: tom de espera local de 440 Hz
  (`cmd/server/meowaudio.go:holdToneSource`) — `moh_url` é aceito no corpo
  mas **não decodificado** (seria preciso um decoder de áudio pra arquivo
  remoto arbitrário; ver nota na descrição do `openapi.yaml`). Testado ao
  vivo: a transição active→held→(tentativa de)active funciona e o estado via
  GET bate; a chamada de teste acabou caindo com `reason=timeout` antes do
  unhold, mas esse MESMO padrão de timeout (~30-40s depois do último pacote
  de entrada real) já tinha aparecido várias vezes nesta sessão em chamadas
  **sem** hold nenhum envolvido (o número de teste fica sem ninguém
  ativamente falando) — não dá pra afirmar com certeza que foi o hold que
  causou a queda, mas também não dá pra descartar 100% sem um humano de
  verdade testando dos dois lados.
- **`POST .../pickup`**: **testado ao vivo com uma segunda sessão pareada**
  (2º device linkado na mesma conta) e **achou um bug real, já corrigido**:
  fechar o bridge antigo (`oldB.Close()`) disparava `pc.OnICEConnectionStateChange`
  → `OnTerminalICE` → `terminateCall` → `Hangup()`, ou seja, o pickup
  derrubava a chamada WhatsApp inteira em vez de só trocar quem tá
  conectado. Corrigido com `Bridge.CloseQuiet()` (`cmd/server/bridge.go`,
  flag `quiet atomic.Bool`), usado tanto no pickup quanto na troca normal de
  bridge em `Session.setBridge` (que tinha o MESMO bug latente — qualquer
  reconexão de WebRTC numa chamada já ativa, tipo reload do browser, também
  derrubava a chamada antes desse fix, só nunca tinha sido exercitado).
  Depois do fix: chamada continua `active` após o pickup, dono reatribuído
  de verdade (dono antigo recebe 409 "claimed by another client", não 404),
  confirmado via curl direto.
- **`POST .../transfer`**: implementado (hold automático + nova perna
  sainte na sessão destino + observação assíncrona via `OnPeerAccept`,
  falha/timeout de 60s restaura o hold) e revisado, mas **não validado de
  ponta a ponta ao vivo**: toda chamada de teste pro número de teste
  (`5521973750831`) nesta sessão morreu com `reason=timeout` uns 20-40s
  depois de atendida — muito curto pra sobreviver até eu conseguir disparar
  o transfer e ainda observar o desfecho assíncrono (aceite/rejeição/60s).
  Numa tentativa a chamada chegou a ser rejeitada de cara pelo telefone de
  teste (provavelmente cansaço de tanta ligação de teste em sequência nesta
  sessão — parei de insistir pra não incomodar). O caminho síncrono
  (validação de request, hold, nova chamada sainte) é o mesmo padrão já
  testado em `doHold`/`doPickup`, mas o fluxo assíncrono completo (aceite
  via `OnPeerAccept`, ou timeout de 60s com rollback) fica sem confirmação
  ao vivo.
- **`X-API-Key` / `WACALLS_API_KEY`**: testado ao vivo (env não setado →
  API aberta, confirmado).

## [ABERTO 2026-09-18] Chamada em grupo trava em "ringing" se 1 de N alvos rejeita

**Sintoma:** numa chamada de grupo com 2+ alvos remotos, se **qualquer um**
deles rejeitar, a chamada inteira fica orfã: a UI trava pra sempre em
"ringing" com vídeo preto, mesmo que outro participante tenha atendido de
verdade e esteja mandando mídia.

**Reproduzido ao vivo** no grupo "Diário da gratidão" (3 membros: eu + 2
remotos). Disquei vídeo de grupo (`GroupCallByIDWithOptions`, 2 alvos):

```
INF group offer sent call_id=4D5110CC... target_count=2 video=true
INF peer rejected call call_id=4D5110CC... from=207821052350663:71@lid
DBG dropping group update for unknown or ended call call_id=4D5110CC...
```

Só que o **outro** alvo (`93497445064726@lid`) atendeu de verdade logo
depois, e o WhatsApp continuou mandando eventos reais da chamada — só que
agora todos são descartados porque o estado interno já foi removido:

```
DBG inbound video state call_id=4D5110CC... orientation=0 state=1
DBG mute_v2 observed; call not awaiting accept call_id=4D5110CC...
WRN group rekey ingest failed error="meowcaller: unknown group call 4D5110CC..."
```

Isso se repetiu por ~2 minutos (vídeo/mute reais chegando, tudo descartado)
até eu encerrar manualmente pela UI. A pré-visualização da câmera local
funcionava (captura própria não depende desse estado), mas o vídeo remoto
nunca apareceu e o badge de status nunca saiu de "ringing".

**Causa raiz:** as linhas de log acima (`group offer sent`, `peer rejected
call`, `dropping group update for unknown or ended call`, `group rekey
ingest failed`) são do **meowcaller** (a dependência externa, via
`meowLogger()`/zerolog), não do nosso código em `cmd/server`. O engine dele
parece remover a chamada do mapa interno (`e.calls`) assim que recebe UMA
rejeição, mesmo que a chamada tenha sido ofertada pra `target_count=2`.
Não é algo que dá pra corrigir editando o WaCalls — teria que ser reportado
upstream no `github.com/purpshell/meowcaller`, ou contornado (ex.: filtrar
alvos offline/improváveis antes de discar, pra reduzir a chance de rejeição
parcial).

**Não corrigido.** Grupos de 2 pessoas (1 alvo remoto) nem chegam a testar
isso — falham antes, de forma limpa, com "requires at least two remote
members" (ver o teste que motivou aumentar o grupo pra 3 membros). O bug só
aparece com 3+ membros e rejeição parcial.

## [REMOVIDO 2026-09-18] Compartilhar tela derruba a chamada inteira

**Sintoma:** clicar em "Share screen" numa chamada de vídeo direta (1:1,
não-grupo) derruba a chamada por completo, não só a feature de tela.

**Causa raiz confirmada por reprodução direta:** `ac.call.StartScreenShare(nil)`
(`cmd/server/httpapi.go:336`, `doScreenShare`) chama `meowcaller`'s
`engine.setScreenShare` → `signaling.BuildScreenShare`
(`meowcaller/signaling/participant_state.go:71`), que manda um nó de
call-control `<screen_share>` pro WhatsApp. Reproduzido isolando o backend
(curl direto em `/api/sessions/{sid}/calls/{id}/screenshare`, sem tocar em
nada do browser/getDisplayMedia): ~1s depois o log mostra

```
WRN call rejected by server call_id=... error_code=404
DEBUG browser ice state state=closed
WRN media ended error="context canceled" call_id=...
INF call terminated call_id=... reason=timeout
```

Ou seja, o servidor do WhatsApp rejeita o nó `<screen_share>` numa chamada
direta e isso derruba a chamada inteira (não é só a feature que falha).
O código-fonte de referência citado no comentário do meowcaller pra esse nó
é literalmente `group-video-reactions.md` — forte indício de que
`screen_share` (e possivelmente `raise_hand`, mesmo datasheet) só é válido
dentro de uma **chamada de grupo** de verdade no protocolo do WhatsApp.
`raise_hand` (`doHandRaise`) foi testado numa chamada direta e **não**
derrubou a ligação nos testes feitos até agora — mas não foi isolado com
o mesmo rigor (sem repro direto via curl), então não descartar a mesma
causa aí também sem verificar.

**Decisão (2026-09-18):** em vez de investigar mais, a feature de
compartilhar tela foi **removida por completo** (usuário pediu pra tirar).
Reações, levantar a mão e chamada em grupo continuam, só screen share saiu:

- Backend: removidos `doScreenShare`/`handleScreenShareStart`/
  `handleScreenShareStop` (`cmd/server/httpapi.go`), rota `/screenshare`,
  `Broker.emitScreenShare` (`broker.go`), `c.OnScreenShare` em
  `wireCall` (`session.go`).
- Frontend: removidos `startScreenShare`/`stopScreenShare`
  (`services/calls.ts`), `useScreenShare` (`hooks/useCallActions.ts`),
  botão de tela em `CallCard.tsx`, `switchToScreen`/`switchToCamera` em
  `lib/video-pipe.ts` e `lib/webrtc.ts`, evento `call-screenshare` em
  `lib/event-stream.ts`, campo `screenSharing` em `stores/calls.ts`.

Se um dia quiser reavaliar: a rota mais promissora seria promover a
chamada direta pra ad-hoc group call via `AddParticipant` antes de
compartilhar tela, já que o comentário do `Call` em `livecall.go` diz
"A direct call may become an ad-hoc group call through AddParticipant".

## [RESOLVIDO 2026-09-17] Vídeo de saída não renderiza no WhatsApp

**Status:** resolvido em 2026-09-17, ao trocar o motor de chamada inteiro
(SRTP/SRTCP/RTP/H264 custom-feito) pelo [meowcaller](https://github.com/purpshell/meowcaller)
como dependência Go (ver `cmd/server/session.go`, `httpapi.go`,
`callregistry.go`, `meowaudio.go`). Vídeo bidirecional confirmado
funcionando numa chamada real.

A causa raiz da parte final **não** era a camada hbh/NACK investigada
abaixo (essa investigação vira arquivo morto, mantida só de referência caso
o motor RTP/SRTP customizado seja revisitado no futuro) — era muito mais
simples: o decoder WebCodecs do navegador (`client/src/lib/video-pipe.ts`)
só decodifica a partir de um quadro marcado como `keyframe`, e precisa de
um timestamp real e crescente por quadro. O código novo
(`cmd/server/httpapi.go`, `doWebRTC`) nunca setava `Keyframe`/`TimestampMS`
ao repassar os quadros H264 do meowcaller pro `Bridge.WriteVideo` — todo
quadro chegava e era descartado em silêncio pelo navegador, pra sempre.
Fix: detectar IDR com `rtp.AUHasIDR` (exportado pelo meowcaller, aliás
**portado originalmente do próprio WaCalls** — ver o comentário em
`meowcaller/rtp/h264.go`) e carimbar `TimestampMS` com o tempo decorrido
desde o início do vídeo na chamada.

### Investigação anterior (arquivada — não foi a causa raiz final)

O texto abaixo documenta a investigação da camada de criptografia "fast"
NACK/hbh no motor **customizado** (SRTP/SRTCP feito à mão no WaCalls, antes
da troca pro meowcaller). Mantido como referência histórica/técnica, não
como problema em aberto — esse motor não está mais em uso pelo servidor.

### Pista forte (2026-09-17, não implementada ainda): camada de cripto separada "hop-by-hop" (hbh)

Rodando o console web do meowcaller direto (`meowcaller/examples/web`) numa
chamada de vídeo real (mesmo peer de teste), a resposta do WhatsApp ao
upgrade de vídeo trouxe, dentro do nó `<relay>`, um campo **`<hbh_key>`**
de 30 bytes — separado do `<key>`/`<auth_token>` normais que o WaCalls já
captura. `RelayConfig` (`internal/voip/transport/sctprelay.go`) **nunca
teve esse campo** — não é parseado em lugar nenhum do código de sinalização
(`internal/voip/signaling/relayack.go`).

O meowcaller já tem a implementação completa dessa camada em
`meowcaller/srtp/hbh.go` (código MIT, não é segredo — só o valor da chave
capturada na chamada é secreto, esse eu não reproduzo em lugar nenhum):

- KDF de dois estágios com HKDF-SHA256 (`deriveHbhSrtpKeyWithLabels`):
  estágio 1 deriva o salt SRTCP a partir do master salt (14B) do `hbh_key`
  com o info `"uplink hbh srtcp salt"` / `"downlink hbh srtcp salt"`;
  estágio 2 deriva a chave de 30 bytes a partir do master key (16B) usando
  esse salt e o info `"uplink hbh srtcp key"` / `"downlink hbh srtcp key"`.
  **Diferente** do KDF de label único (RFC 3711 `aesCmKdf`) que o WaCalls
  usa hoje pro SRTCP principal.
- Cifra própria "estilo libsrtp" (`aesICMCrypt`/`ExpandLibsrtpSessionKeys`):
  AES-ICM com contador de 2 níveis (incrementa byte 15, carrega pro byte 14
  só no overflow) — explicitamente **não é** AES-CTR de 128 bits padrão
  (comentário do próprio meowcaller marca isso como "load-bearing").
- Direções separadas: uplink e downlink derivam chaves diferentes a partir
  do mesmo `hbh_key`.
- **Validado contra vetores de teste conhecidos** (`meowcaller/srtp/hbh_test.go`
  + `testdata/kats.json`, cruzado com a referência
  [whatsapp-rust](https://github.com/oxidezap/whatsapp-rust)) — rodei
  `go test ./srtp/... -run TestHbh` no meowcaller, os dois testes passam.
  Não é especulação, é uma implementação corretamente verificada.

**O que isso explica**: áudio, vídeo, REMB, SR, PLI/FIR decifram bem com a
chave "principal" (E2E per-participant, já implementada certa no WaCalls).
Só o NACK isolado falha sempre. Se o NACK "fast" trafega nessa camada hbh
separada (chave/KDF/cifra totalmente diferentes), faz todo sentido que
nenhum candidato de SSRC testado contra a chave principal desse resultado
plausível — a chave errada nunca foi essa, era uma cifra inteira diferente.

**Ressalva**: o próprio meowcaller define essa camada (`srtp/hbh.go`) mas
**não a usa em produção** — só existe testada isoladamente contra os KATs,
nunca foi de fato ligada ao caminho de NACK real do meowcaller (que nem
implementa NACK). Ou seja: essa é uma pista forte e bem fundamentada, não
uma confirmação — falta (1) capturar `hbh_key` na sinalização do WaCalls,
(2) portar essa derivação de chave/cifra, (3) usá-la especificamente pro
`Unprotect` do canal de NACK isolado, e (4) testar contra uma chamada real
com `-video-dump` pra confirmar que os `seqs` decodificados batem com o
que foi mandado de verdade.

### O que já foi descartado (investigação anterior, antes da pista do hbh)

### Sintoma

Numa chamada de vídeo real (browser → WaCalls → WhatsApp), o outro lado atende e
recebe a ligação normalmente, mas nunca renderiza o vídeo de saída. Confirmado
com `-video-dump` numa chamada real: o WhatsApp manda `RTPFB/NACK` (PT=205,
FMT=1) pedindo retransmissão dos pacotes de vídeo que não chegaram, mas
`SrtcpContext.Unprotect` nunca conseguia validar o tag de autenticação desses
pacotes — `"srtcp: auth tag inválido"` em 100% dos NACKs recebidos.

### O que já foi descartado

Comparado a fundo com a implementação de referência
[`meowcaller`](https://github.com/purpshell/meowcaller) (cópia local em
`/home/fabricio/Documentos/meowcaller`, ver [docs/REFERENCES.md](../docs/REFERENCES.md)):

- **A fórmula de derivação de chave está correta.** `DerivePerJidSrtpKey`
  (`internal/voip/media/encryption.go`) e `deriveSrtpKey`
  (`internal/voip/media/srtp.go`) usam o mesmo esquema que
  `deriveSessionKeysFromMasterLabels` do meowcaller: HKDF-SHA256 do
  `callKey`, rótulos RFC 3711 0x03 (cifra)/0x04 (auth)/0x05 (salt) pro
  SRTCP. Comparação feita em `meowcaller/srtp/e2e.go`.
- **O SSRC usado pra derivar o IV está correto.** O meowcaller
  (`rtp.ParseRtcpSenderSsrc`, `meowcaller/rtp/rtcp.go:48`) usa direto o SSRC
  em claro nos bytes 4-7 do pacote — exatamente o candidato `wire_claro`
  que `debugTryNackSsrcCandidates` (`internal/voip/call/videodump.go`) já
  testava. Não é questão de SSRC errado.
- **O campo `key` por-relay do offer é outra coisa.** `RelayConfig.Key`
  (`internal/voip/transport/sctprelay.go`) autentica o `ice-pwd`/HMAC da
  conexão SCTP/WebRTC com o relay — não tem relação com a criptografia de
  mídia SRTP/SRTCP. Pista descartada.
- **A mesma chave/contexto decifra REMB, SR/RR e PLI/FIR sem problema** —
  só falha em pacotes NACK isolados (30-34 bytes, só o RTPFB, não dentro de
  um pacote composto). Isso indica que o problema não é a chave em si (se
  fosse, tudo falharia), e sim algo específico do canal/contexto que carrega
  o NACK — possivelmente um SSRC/dispositivo/relay diferente do stream
  principal de vídeo que ainda não foi identificado.
- **O meowcaller não implementa retransmissão por NACK** (só reage a
  PLI/FIR pedindo keyframe), então não existe uma implementação de
  referência pra comparar diretamente o caminho de decifração do NACK
  isolado.
- **Força bruta de SSRC candidato (`debugTryNackSsrcCandidates`) não é
  confiável pra validar um candidato.** Testado ao vivo em 2026-09-17: um
  NACK isolado de 16 bytes carrega só 1 entrada FCI (RFC 4585) — 1 PID de
  16 bits independente, o resto de `seqs` é derivado dele (PID+bitmap), não
  são valores independentes. A checagem de "plausível" (seq caiu numa
  janela recente do que mandamos) tem ~3% de chance de bater por acaso
  nesse caso; com ~10 candidatos testados por pacote, ver "candidato
  plausível" a cada poucos segundos é estatisticamente esperado, não sinal
  de verdade — confirmado ao vivo: candidatos diferentes (`self_audio`,
  `self_video`, `peer_video`, `wire_claro`, `zero`) "bateram" em momentos
  diferentes da mesma chamada, nenhum de forma consistente. O código
  (`internal/voip/call/videodump.go`) agora só marca como `CANDIDATO
  PLAUSÍVEL` (WARN) quando o pacote tem 2+ entradas FCI batendo juntas —
  reduz bastante o falso positivo, mas a maioria dos NACKs reais vistos até
  agora é de 1 entrada só, então essa via de investigação segue limitada.

### Mitigação atual (temporária, não commitada)

Pra não bloquear a chamada real enquanto isso não é resolvido,
`SrtcpContext.Unprotect` (`internal/voip/media/srtcp.go`) foi ajustado pra
**não bloquear mais** em tag de auth inválida: devolve o pacote decifrado
mesmo assim, sinalizando via `media.ErrAuthTagMismatch` (erro sentinela,
não nil, mas com dado válido). `handleInboundRtcp`
(`internal/voip/call/callmanager_rtcp.go`) trata esse erro especificamente:
loga um aviso (`level=WARN`, sempre, não só sob `-video-dump`) e continua
processando o pacote normalmente, em vez de descartá-lo.

Isso restaura o comportamento de antes do commit `3e0587e` (que adicionou a
validação de auth tag como parte de um hardening de segurança — ver
`git log` e a mensagem desse commit) especificamente para o caso de
mismatch, mas mantém a validação/log visível para continuar a investigação.

**Estado no repo:** essa mitigação está nos arquivos de trabalho, ainda
**não commitada**:
`internal/voip/media/srtcp.go`, `internal/voip/media/srtcp_test.go`,
`internal/voip/call/callmanager_rtcp.go`, `internal/voip/call/videodump.go`
(ferramenta de candidatos SSRC com filtro de plausibilidade por 2+ entradas
FCI), `internal/voip/call/callmanager.go` e `callmanager_video.go`
(`videoTxLastSeq`, o seq real do último pacote de vídeo mandado — usado
pra plausibilidade em vez de `txPkts`, que não serve de referência porque
o seq RTP começa aleatório). Revisar e commitar quando fizer sentido.

### Próximos passos sugeridos

1. A força bruta de SSRC via `-video-dump` está no limite do que rende
   sinal (ver acima) — não repetir esse teste esperando um resultado
   diferente. Só voltaria a valer a pena se aparecer um NACK real com 2+
   entradas FCI (pacote maior, ~20+ bytes decifrados) pra testar os
   candidatos com uma janela de confiança maior.
2. Considerar abrir uma issue/discussão na comunidade do meowcaller (Matrix
   `#meowcaller:matrix.org` ou Discord, citados no README dele) perguntando
   especificamente sobre o canal de NACK isolado — como ninguém ali
   implementou isso ainda, pode ser um convite pra colaboração.
3. Investigar se o peer tem mais de um dispositivo/SSRC ativo mandando
   feedback (`peer=...@lid` com sufixo de device — ver os logs de chamada)
   e se o NACK vem de um SSRC/relay diferente do stream de vídeo principal
   — ainda não testado a fundo.
4. Quando a chave/contexto certo for identificado, reverter a mitigação
   (voltar `Unprotect` a bloquear de verdade em auth inválido) — o
   `ErrAuthTagMismatch` já está isolado para isso ser uma mudança pequena.
