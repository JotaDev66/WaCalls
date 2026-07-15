export type Messages = {
  app: {
    noAccountsTitle: string;
    noAccountsDescription: string;
    selectAccountTitle: string;
    selectAccountDescription: string;
  };
  onboarding: {
    title: string;
    subtitle: string;
    dismiss: string;
    link: string;
    createCta: string;
    call: string;
  };
  header: {
    accounts: string;
    toggleTheme: string;
    toggleLanguage: string;
  };
  common: {
    cancel: string;
    confirm: string;
    delete: string;
    loading: string;
  };
  auth: {
    title: string;
    description: string;
    tokenPlaceholder: string;
    submit: string;
  };
  sessions: {
    accounts: string;
    newSession: string;
    noAccounts: string;
    deleteAria: (name: string) => string;
    deleteTitle: string;
    deleteDescription: (name: string) => string;
    disconnect: string;
    reactivate: string;
    status: {
      open: string;
      qr: string;
      connecting: string;
      logged_out: string;
    };
  };
  pairing: {
    title: (name: string) => string;
    description: string;
    disconnectedBadge: string;
    waitingQr: string;
  };
  calls: {
    activeLabel: (n: number) => string;
    noCallsTitle: string;
    noCallsDescription: string;
    otherActive: string;
    status: {
      ringing: string;
      starting: string;
      reconnecting: string;
      ended: string;
    };
    direction: { inbound: string; outbound: string };
    reconnectingMedia: string;
    reconnectWhy: string;
    reconnectHint: string;
    measuringQuality: string;
    connectedIn: string;
    connection: string;
    mic: string;
    peer: string;
    rtt: string;
    jitter: string;
    loss: string;
    endCall: string;
  };
  dialer: {
    title: string;
    phonePlaceholder: string;
    call: string;
    calling: string;
    defaultMic: string;
    defaultSpeaker: string;
  };
  history: {
    button: string;
    title: string;
    exportCsv: string;
    emptyTitle: string;
    emptyDescription: string;
    loadMore: string;
    loading: string;
  };
  incoming: {
    title: string;
    accept: string;
    reject: string;
  };
  connection: {
    reconnecting: string;
  };
  omnibox: {
    placeholder: string;
    dial: (phone: string) => string;
    switchTo: (name: string) => string;
    empty: string;
  };
};
