import type { Messages } from "./messages";

export const ptBR: Messages = {
  app: {
    noAccountsTitle: "Nenhuma conta ainda",
    noAccountsDescription:
      "Crie sua primeira conta do WhatsApp na barra lateral para começar a ligar.",
    selectAccountTitle: "Selecione uma conta",
    selectAccountDescription: "Escolha uma conta na barra lateral.",
  },
  header: {
    accounts: "Contas",
    toggleTheme: "Alternar tema",
    toggleLanguage: "Mudar para inglês",
  },
  common: {
    cancel: "Cancelar",
    confirm: "Confirmar",
    delete: "Excluir",
    loading: "Carregando…",
  },
  auth: {
    title: "Autenticação necessária",
    description: "Informe o token da API para continuar.",
    tokenPlaceholder: "Token da API",
    submit: "Salvar e recarregar",
  },
  sessions: {
    accounts: "Contas",
    newSession: "Nova sessão",
    noAccounts: "Nenhuma conta ainda.",
    deleteAria: (name) => `Excluir ${name}`,
    deleteTitle: "Excluir conta?",
    deleteDescription: (name) => `${name} será desconectada e removida.`,
    disconnect: "Desconectar",
    reactivate: "Reativar",
    status: {
      open: "Conectado",
      qr: "Ler QR",
      connecting: "Conectando…",
      logged_out: "Desconectado",
    },
  },
  pairing: {
    title: (name) => `Parear ${name}`,
    description:
      "Abra o WhatsApp → Aparelhos conectados → Conectar um aparelho e escaneie.",
    disconnectedBadge: "Desconectado. Use Reativar acima para obter um QR",
    waitingQr: "Aguardando QR…",
  },
  calls: {
    activeLabel: (n) =>
      `chamada${n === 1 ? "" : "s"} ativa${n === 1 ? "" : "s"}`,
    noCallsTitle: "Nenhuma chamada ativa",
    noCallsDescription: "Disque um número acima para iniciar uma chamada.",
    otherActive: "Outras chamadas ativas",
    status: {
      ringing: "chamando",
      starting: "iniciando",
      reconnecting: "reconectando",
      ended: "encerrada",
    },
    direction: { inbound: "recebida", outbound: "realizada" },
    reconnectingMedia: "Reconectando mídia…",
    measuringQuality: "Medindo qualidade…",
    connectedIn: "Conectado em",
    connection: "Conexão",
    mic: "Mic",
    peer: "Remoto",
    rtt: "RTT",
    jitter: "Jitter",
    loss: "Perda",
    endCall: "Encerrar chamada",
  },
  dialer: {
    title: "Discador",
    phonePlaceholder: "+55 11 99999 9999",
    call: "Ligar",
    calling: "Ligando…",
    defaultMic: "Microfone padrão",
    defaultSpeaker: "Alto-falante padrão",
  },
  history: {
    button: "Histórico",
    title: "Histórico de chamadas",
    exportCsv: "Exportar CSV",
    emptyTitle: "Nenhuma chamada anterior",
    emptyDescription: "As chamadas que você fizer ou receber aparecerão aqui.",
    loadMore: "Carregar mais",
    loading: "Carregando…",
  },
  incoming: {
    title: "Chamada recebida",
    accept: "Atender",
    reject: "Recusar",
  },
  connection: {
    reconnecting: "Reconectando ao servidor…",
  },
};
