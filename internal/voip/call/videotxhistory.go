package call

// videoTxHistory guarda os últimos pacotes de vídeo já protegidos (SRTP), por
// sequence number, para retransmiti-los quando o WhatsApp pede via RTCP NACK.
//
// O canal do relay é SCTP ordenado, então a perna navegador→relay não perde
// pacote; mas a perna relay→celular perde. Um keyframe H264 fragmentado em
// dezenas de pacotes FU-A nunca fecha no decodificador do WhatsApp se um único
// fragmento se perde e ninguém retransmite — era o que faltava para o vídeo de
// saída renderizar. Retransmissão simples no mesmo SSRC/PT (não há RTX
// negociado, que exigiria SDP que este caminho não tem).
//
// Não é seguro para uso concorrente; o CallManager só mexe nela sob m.mu.
type videoTxHistory struct {
	buf  [][]byte
	seqs []uint16
	set  []bool
}

// videoTxHistorySize cobre ~1s de vídeo a 30fps mesmo com um keyframe grande
// (um keyframe em 640px raramente passa de ~40 pacotes); é potência de 2 para o
// índice sair de um AND.
const videoTxHistorySize = 1024

func newVideoTxHistory() *videoTxHistory {
	return &videoTxHistory{
		buf:  make([][]byte, videoTxHistorySize),
		seqs: make([]uint16, videoTxHistorySize),
		set:  make([]bool, videoTxHistorySize),
	}
}

// put guarda uma cópia do pacote protegido no slot do seq, sobrescrevendo o
// ocupante antigo daquele slot.
func (h *videoTxHistory) put(seq uint16, pkt []byte) {
	i := seq & (videoTxHistorySize - 1)
	cp := make([]byte, len(pkt))
	copy(cp, pkt)
	h.buf[i] = cp
	h.seqs[i] = seq
	h.set[i] = true
}

// get devolve o pacote protegido guardado para seq, ou ok=false se ele já foi
// reciclado (o slot agora guarda outro seq) ou nunca existiu.
func (h *videoTxHistory) get(seq uint16) ([]byte, bool) {
	i := seq & (videoTxHistorySize - 1)
	if !h.set[i] || h.seqs[i] != seq {
		return nil, false
	}
	return h.buf[i], true
}
