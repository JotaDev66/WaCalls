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
    backspace: string;
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
  nav: {
    console: string;
    contacts: string;
  };
  contacts: {
    title: string;
    searchPlaceholder: string;
    loading: string;
    empty: string;
    emptyHint: string;
    noResults: string;
    error: string;
    retry: string;
    refresh: string;
    call: string;
    callAria: (name: string) => string;
    pick: string;
    pickTitle: string;
    newContact: string;
    editContact: string;
    phoneLabel: string;
    nameLabel: string;
    save: string;
    saving: string;
    notOnWhatsApp: string;
    appStateSyncing: string;
    saveError: string;
    saveSuccess: string;
    phoneRequired: string;
    nameRequired: string;
    duplicateNotice: (name: string) => string;
    editThisInstead: string;
    editAria: (name: string) => string;
  };
  login: {
    title: string;
    description: string;
    user: string;
    password: string;
    submit: string;
    submitting: string;
    error: string;
  };
  password: {
    title: string;
    current: string;
    next: string;
    submit: string;
    saving: string;
    success: string;
    error: string;
    menu: string;
  };
  account: { logout: string };
};
