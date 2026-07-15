export type Contact = {
  jid: string;
  name: string;
  phone: string;
  photoUrl?: string;
};

export type SaveContactInput = {
  phone: string;
  name: string;
};
