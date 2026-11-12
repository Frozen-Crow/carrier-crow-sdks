import { createHmac, randomUUID, timingSafeEqual } from "node:crypto";
import type {
  SendEmail,
  SendResult,
  BatchResponse,
  EmailQuery,
  EmailList,
  EmailDetail,
  EmailSummary,
  Domain,
  DomainList,
  WebhookBody,
} from "./models.js";
export * from "./models.js";
export const VERSION = "0.1.0";
export class CarrierCrowError extends Error {
  constructor(
    message: string,
    public readonly status: number = 0,
    public readonly code: string = "network_error",
    public readonly field?: string,
    public readonly retryAfter?: number,
    options?: ErrorOptions,
  ) {
    super(message, options);
    this.name = "CarrierCrowError";
  }
}
export interface ClientOptions {
  baseUrl?: string;
  /** Retry count for reads and sends with an explicit key. Default 2. */ maxRetries?: number;
  timeout?: number;
  /** Initial backoff in milliseconds. Default 500. */ retryDelay?: number;
}
export interface SendOptions {
  idempotencyKey?: string;
  /** Opt in to retries; generates an idempotency key when absent. */ retries?: number;
}
export interface VerifyOptions {
  tolerance?: number;
  /** Unix seconds, for deterministic verification. */ now?: number;
}
const sleep = (ms: number) =>
  new Promise<void>((resolve) => setTimeout(resolve, ms));
function count(value: number, name: string) {
  if (!Number.isInteger(value) || value < 0)
    throw new CarrierCrowError(
      `${name} must be a nonnegative integer`,
      0,
      "invalid_request",
      name,
    );
  return value;
}
export const webhooks = {
  verify(
    body: string | Uint8Array,
    signatureHeader: string,
    secret: string,
    options: VerifyOptions = {},
  ): WebhookBody {
    const fail = (message: string): never => {
      throw new CarrierCrowError(message, 0, "invalid_signature");
    };
    const tolerance = options.tolerance ?? 300;
    const now = options.now ?? Date.now() / 1000;
    if (!Number.isFinite(tolerance) || tolerance < 0 || !Number.isFinite(now))
      return fail("Invalid webhook clock or tolerance");
    const parts = signatureHeader.split(",").map((p) => p.trim());
    const timestamps = parts.filter((p) => p.startsWith("t="));
    if (timestamps.length !== 1 || !/^t=\d+$/.test(timestamps[0]))
      return fail("Missing or invalid webhook timestamp");
    const timestamp = timestamps[0].slice(2);
    if (
      !Number.isSafeInteger(Number(timestamp)) ||
      Math.abs(now - Number(timestamp)) > tolerance
    )
      return fail("Webhook timestamp is outside tolerance");
    const bytes =
      typeof body === "string" ? Buffer.from(body, "utf8") : Buffer.from(body);
    const expected = createHmac("sha256", secret)
      .update(`${timestamp}.`)
      .update(bytes)
      .digest();
    const valid = parts
      .filter((p) => p.startsWith("v1="))
      .some((p) => {
        const hex = p.slice(3);
        return (
          /^[a-fA-F0-9]{64}$/.test(hex) &&
          timingSafeEqual(expected, Buffer.from(hex, "hex"))
        );
      });
    if (!valid) return fail("Webhook signature does not match");
    try {
      const parsed: unknown = JSON.parse(bytes.toString("utf8"));
      if (
        !parsed ||
        typeof parsed !== "object" ||
        typeof (parsed as WebhookBody).webhook !== "string" ||
        typeof (parsed as WebhookBody).delivery !== "string" ||
        !Array.isArray((parsed as WebhookBody).events)
      )
        throw new Error("Invalid envelope");
      return parsed as WebhookBody;
    } catch (cause) {
      throw new CarrierCrowError(
        "Webhook body is not a valid JSON envelope",
        0,
        "invalid_webhook",
        undefined,
        undefined,
        { cause },
      );
    }
  },
};
export class CarrierCrow {
  private readonly key: string;
  private readonly baseUrl: string;
  private readonly maxRetries: number;
  private readonly timeout: number;
  private readonly retryDelay: number;
  readonly webhooks = webhooks;
  constructor(
    apiKey: string | undefined = process.env.CARRIERCROW_API_KEY,
    options: ClientOptions = {},
  ) {
    if (!apiKey)
      throw new CarrierCrowError(
        "Pass an API key or set CARRIERCROW_API_KEY",
        0,
        "unauthorized",
      );
    this.key = apiKey;
    this.baseUrl = (
      options.baseUrl ?? "https://api.mta.carriercrow.com"
    ).replace(/\/+$/, "");
    this.maxRetries = count(options.maxRetries ?? 2, "maxRetries");
    this.timeout = options.timeout ?? 30000;
    this.retryDelay = options.retryDelay ?? 500;
    if (
      !Number.isFinite(this.timeout) ||
      this.timeout <= 0 ||
      !Number.isFinite(this.retryDelay) ||
      this.retryDelay < 0
    )
      throw new CarrierCrowError(
        "Invalid timeout or retryDelay",
        0,
        "invalid_request",
      );
  }
  readonly emails = {
    send: (params: SendEmail, options: SendOptions = {}): Promise<SendResult> =>
      this.sendRequest("/v1/emails", params, options),
    batch: (
      emails: SendEmail[],
      options: SendOptions = {},
    ): Promise<BatchResponse> =>
      this.sendRequest("/v1/emails/batch", { emails }, options),
    get: (id: string): Promise<EmailDetail> =>
      this.request("GET", `/v1/emails/${encodeURIComponent(id)}`),
    list: (query: EmailQuery = {}): Promise<EmailList> => {
      const search = new URLSearchParams();
      for (const [key, value] of Object.entries(query)) {
        if (value !== undefined)
          for (const item of Array.isArray(value) ? value : [value])
            search.append(key, String(item));
      }
      return this.request(
        "GET",
        `/v1/emails${search.toString() ? `?${search}` : ""}`,
      );
    },
    pages: (query: EmailQuery = {}): AsyncGenerator<EmailList> =>
      this.pages(query),
    iterate: (query: EmailQuery = {}): AsyncGenerator<EmailSummary> =>
      this.iterate(query),
  };
  readonly domains = {
    list: (): Promise<DomainList> => this.request("GET", "/v1/domains"),
    get: (id: string): Promise<Domain> =>
      this.request("GET", `/v1/domains/${encodeURIComponent(id)}`),
    check: (id: string): Promise<Domain> =>
      this.request("POST", `/v1/domains/${encodeURIComponent(id)}/check`),
  };
  private async *pages(query: EmailQuery): AsyncGenerator<EmailList> {
    let before = query.before;
    const seen = new Set<string>();
    while (true) {
      const page = await this.emails.list({ ...query, before });
      yield page;
      if (!page.next) return;
      if (seen.has(page.next))
        throw new CarrierCrowError(
          "Server repeated a pagination cursor",
          0,
          "invalid_response",
        );
      seen.add(page.next);
      before = page.next;
    }
  }
  private async *iterate(query: EmailQuery): AsyncGenerator<EmailSummary> {
    for await (const page of this.pages(query)) yield* page.emails;
  }
  private sendRequest<T>(
    path: string,
    body: unknown,
    options: SendOptions,
  ): Promise<T> {
    const retries =
      options.retries === undefined
        ? this.maxRetries
        : count(options.retries, "retries");
    const key =
      options.idempotencyKey ??
      (options.retries !== undefined && retries > 0 ? randomUUID() : undefined);
    if (key !== undefined && (key.length < 1 || key.length > 255))
      throw new CarrierCrowError(
        "Idempotency key must have 1–255 characters",
        0,
        "invalid_request",
        "idempotencyKey",
      );
    return this.request("POST", path, body, key ? retries : 0, key);
  }
  private async request<T>(
    method: string,
    path: string,
    body?: unknown,
    retries = this.maxRetries,
    idempotencyKey?: string,
  ): Promise<T> {
    const serialized = body === undefined ? undefined : JSON.stringify(body);
    for (let attempt = 0; ; attempt++) {
      let failure: CarrierCrowError;
      try {
        const headers: Record<string, string> = {
          Authorization: `Bearer ${this.key}`,
          "User-Agent": `carriercrow-node/${VERSION}`,
          Accept: "application/json",
        };
        if (serialized !== undefined)
          headers["Content-Type"] = "application/json";
        if (idempotencyKey !== undefined)
          headers["Idempotency-Key"] = idempotencyKey;
        const response = await fetch(`${this.baseUrl}${path}`, {
          method,
          headers,
          body: serialized,
          signal: AbortSignal.timeout(this.timeout),
          redirect: "error",
        });
        const text = await response.text();
        let data: Record<string, unknown>;
        try {
          data = JSON.parse(text);
        } catch {
          data = {};
        }
        if (response.ok) {
          if (!text || !Object.keys(data ?? {}).length)
            throw new CarrierCrowError(
              "API returned invalid JSON",
              response.status,
              "invalid_response",
            );
          return data as T;
        }
        const rawRetry = response.headers.get("retry-after");
        const retryAfter =
          rawRetry !== null && /^\d+(\.\d+)?$/.test(rawRetry)
            ? Number(rawRetry)
            : undefined;
        failure = new CarrierCrowError(
          typeof data?.error === "string"
            ? data.error
            : `HTTP ${response.status}`,
          response.status,
          typeof data?.code === "string" ? data.code : "http_error",
          typeof data?.field === "string" ? data.field : undefined,
          retryAfter,
        );
      } catch (cause) {
        failure =
          cause instanceof CarrierCrowError
            ? cause
            : new CarrierCrowError(
                "Network request failed",
                0,
                "network_error",
                undefined,
                undefined,
                { cause },
              );
      }
      if (
        attempt >= retries ||
        ![0, 429, 503].includes(failure.status) ||
        (failure.status === 0 && failure.code !== "network_error")
      )
        throw failure;
      await sleep(
        Math.max(
          (failure.retryAfter ?? 0) * 1000,
          this.retryDelay * 2 ** attempt * (0.5 + Math.random()),
        ),
      );
    }
  }
}
export default CarrierCrow;
