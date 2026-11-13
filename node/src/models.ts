export type Addresses = string | string[];
export interface Attachment {
  filename: string;
  content: string;
  content_type?: string;
}
export interface SendEmail {
  from: string;
  to: Addresses;
  subject: string;
  cc?: Addresses;
  bcc?: Addresses;
  reply_to?: Addresses;
  html?: string;
  text?: string;
  headers?: Record<string, string>;
  attachments?: Attachment[];
  message_id?: string;
  subscriber?: string;
  campaign?: string;
  app?: string;
}
export interface Recipient {
  address: string;
  id: string | null;
}
export interface SendResult {
  object: "email";
  status: "accepted";
  message_id: string | null;
  recipients: Recipient[];
}
export interface BatchResult {
  index: number;
  status: "accepted" | "rejected";
  message_id?: string | null;
  recipients?: Recipient[];
  error?: string;
  code?: string;
  field?: string;
}
export interface BatchResponse {
  results: BatchResult[];
}
export type ErrorCode =
  | "invalid_request"
  | "unauthorized"
  | "forbidden"
  | "app_mismatch"
  | "not_found"
  | "idempotency_conflict"
  | "too_large"
  | "rejected"
  | "rate_limited"
  | "unavailable";
export interface ErrorBody {
  error: string;
  code: ErrorCode;
  field?: string;
}
export type EmailStatus =
  | "delivered"
  | "delayed"
  | "waiting"
  | "bounced"
  | "expired"
  | "complained";
export interface EmailResponse {
  code: number;
  enhanced: string | null;
  text: string;
}
export interface EmailSummary {
  id: string;
  messageId: string | null;
  recipient: string;
  tenant: string;
  campaign: string | null;
  app: string | null;
  status: EmailStatus;
  firstAt: string;
  at: string;
  tries: number;
  provider: string;
  moved: boolean;
  reason: string | null;
  bounceClass: string | null;
  response: EmailResponse | null;
  held: {
    until?: string | null;
    by?: "pause" | "limit" | "no-source" | "other";
  } | null;
}
export interface EmailList {
  emails: EmailSummary[];
  next: string | null;
  since: string;
}
export interface EmailQuery {
  q?: string;
  status?: EmailStatus[];
  tenant?: string;
  app?: string;
  days?: number;
  before?: string;
  limit?: number;
}
export interface EmailEvent {
  at: string;
  kind:
    | "received"
    | "held"
    | "delayed"
    | "delivered"
    | "bounced"
    | "removed"
    | "expired"
    | "complained";
  try?: number;
  response?: EmailResponse;
  reason?: string | null;
  bounceClass?: string | null;
  source?: string | null;
  sourceAddress?: string | null;
  server?: string | null;
  tls?: string | null;
  until?: string | null;
  [key: string]: unknown;
}
export interface EmailDetail {
  email: EmailSummary & {
    from?: string | null;
    returnPath?: string | null;
    subscriber?: string | null;
  };
  events: EmailEvent[];
  updates: Record<string, unknown>[];
}
export interface DnsRecord {
  for: "signature" | "permission" | "fakes";
  type: string;
  name: string;
  value: string;
  status: "found" | "missing" | "wrong";
}
export interface Domain {
  id: string;
  name: string;
  sender: string;
  verified: boolean;
  records: DnsRecord[];
  checked_at: string | null;
}
export interface DomainList {
  domains: Domain[];
}
export interface WebhookEvent {
  id: string;
  type:
    | "message.received"
    | "message.delivered"
    | "message.deferred"
    | "message.bounced"
    | "message.expired"
    | "message.complained";
  at: string;
  tenant: string;
  message: {
    id: string | null;
    mtaId: string | null;
    recipient: string;
    subscriber: string | null;
    campaign: string | null;
  };
  attempts?: number;
  response?: EmailResponse;
  bounce?: { class?: string | null; kind?: "remote" | "admin" | "report" };
  complaint?: { type?: string | null; reporter?: string | null };
}
export interface WebhookBody {
  webhook: string;
  delivery: string;
  test?: boolean;
  events: WebhookEvent[];
}
