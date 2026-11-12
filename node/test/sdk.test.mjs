import test from "node:test";
import assert from "node:assert/strict";
import { createServer } from "node:http";
import { readFileSync } from "node:fs";
import { createHmac } from "node:crypto";
import { createRequire } from "node:module";
import CarrierCrow, { CarrierCrowError, webhooks } from "../dist/esm/index.js";
const email = {
  from: "Sender <a@example.com>",
  to: ["b@example.com"],
  cc: "c@example.com",
  bcc: ["d@example.com"],
  reply_to: "r@example.com",
  subject: "Hi",
  html: "<b>Hello</b>",
  text: "Hello",
  headers: { "X-Custom": "yes" },
  attachments: [
    { filename: "hi.txt", content: "aGk=", content_type: "text/plain" },
  ],
  message_id: "order-1",
  subscriber: "sub",
  campaign: "welcome",
  app: "shop",
};
async function mock(fn, run) {
  const requests = [];
  const server = createServer(async (req, res) => {
    let raw = "";
    for await (const chunk of req) raw += chunk;
    const record = {
      method: req.method,
      url: req.url,
      headers: req.headers,
      raw,
      body: raw ? JSON.parse(raw) : undefined,
    };
    requests.push(record);
    fn(record, res, requests.length);
  });
  await new Promise((r) => server.listen(0, "127.0.0.1", r));
  try {
    await run(
      new CarrierCrow("ccm_test", {
        baseUrl: `http://127.0.0.1:${server.address().port}/`,
        retryDelay: 0,
      }),
      requests,
    );
  } finally {
    server.closeAllConnections();
    await new Promise((r) => server.close(r));
  }
}
const json = (res, body, status = 200, headers = {}) => {
  res.writeHead(status, { "Content-Type": "application/json", ...headers });
  res.end(JSON.stringify(body));
};
test("all seven API methods, full wire body, headers and escaped paths", async () => {
  await mock(
    (req, res) => json(res, { ok: true }),
    async (client, requests) => {
      await client.emails.send(email, { idempotencyKey: "one" });
      await client.emails.batch([email], { idempotencyKey: "batch" });
      await client.emails.get("a/b");
      await client.emails.list({
        q: "@example.com",
        status: ["delayed", "waiting"],
        tenant: "retail",
        app: "shop",
        days: 3,
        before: "cursor x",
        limit: 20,
      });
      await client.domains.list();
      await client.domains.get("d/e");
      await client.domains.check("d/e");
      assert.deepEqual(
        requests.map((r) => [r.method, r.url.split("?")[0]]),
        [
          ["POST", "/v1/emails"],
          ["POST", "/v1/emails/batch"],
          ["GET", "/v1/emails/a%2Fb"],
          ["GET", "/v1/emails"],
          ["GET", "/v1/domains"],
          ["GET", "/v1/domains/d%2Fe"],
          ["POST", "/v1/domains/d%2Fe/check"],
        ],
      );
      assert.deepEqual(requests[0].body, email);
      assert.deepEqual(requests[1].body, { emails: [email] });
      assert.equal(requests[0].headers["idempotency-key"], "one");
      assert.equal(requests[1].headers["idempotency-key"], "batch");
      for (const req of requests) {
        assert.equal(req.headers.authorization, "Bearer ccm_test");
        assert.equal(req.headers["user-agent"], "carriercrow-node/0.1.0");
        assert.equal(req.headers.accept, "application/json");
      }
      assert.equal(requests[0].headers["content-type"], "application/json");
      for (const req of requests.slice(2)) assert.equal(req.raw, "");
      const q = new URL(requests[3].url, "http://test").searchParams;
      assert.deepEqual(q.getAll("status"), ["delayed", "waiting"]);
      for (const [key, value] of Object.entries({
        q: "@example.com",
        tenant: "retail",
        app: "shop",
        days: "3",
        before: "cursor x",
        limit: "20",
      }))
        assert.equal(q.get(key), value);
    },
  );
});
test("typed API errors retain fields and Retry-After; nonretryable errors run once", async () => {
  for (const status of [400, 401, 403, 404, 409, 413, 422, 500])
    await mock(
      (req, res) =>
        json(
          res,
          { error: "Fix recipient", code: "invalid_request", field: "to[2]" },
          status,
          { "Retry-After": "3" },
        ),
      async (client, requests) => {
        await assert.rejects(
          client.emails.get("x"),
          (e) =>
            e instanceof CarrierCrowError &&
            e.status === status &&
            e.code === "invalid_request" &&
            e.message === "Fix recipient" &&
            e.field === "to[2]" &&
            e.retryAfter === 3,
        );
        assert.equal(requests.length, 1);
      },
    );
});
test("429 and 503 retry with bound; Retry-After preserved", async () => {
  for (const status of [429, 503])
    await mock(
      (req, res, n) =>
        json(
          res,
          n < 3 ? { error: "later", code: "unavailable" } : { domains: [] },
          n < 3 ? status : 200,
          { "Retry-After": "0" },
        ),
      async (client, requests) => {
        assert.deepEqual(await client.domains.list(), { domains: [] });
        assert.equal(requests.length, 3);
      },
    );
  await mock(
    (req, res) =>
      json(res, { error: "later", code: "rate_limited" }, 429, {
        "Retry-After": "0",
      }),
    async (client, requests) => {
      await assert.rejects(
        client.domains.list(),
        (e) => e.status === 429 && e.retryAfter === 0,
      );
      assert.equal(requests.length, 3);
    },
  );
});
test("send and batch never retry without key; explicit retry opt-in generates stable key and body", async () => {
  for (const method of ["send", "batch"]) {
    const params = method === "send" ? email : [email];
    await mock(
      (req, res) => json(res, { error: "later", code: "unavailable" }, 503),
      async (client, requests) => {
        await assert.rejects(client.emails[method](params));
        assert.equal(requests.length, 1);
        assert.equal(requests[0].headers["idempotency-key"], undefined);
      },
    );
    for (const options of [{ retries: 1 }, { idempotencyKey: "provided" }])
      await mock(
        (req, res, n) =>
          json(
            res,
            n === 1 ? { error: "later", code: "unavailable" } : { ok: true },
            n === 1 ? 503 : 200,
          ),
        async (client, requests) => {
          await client.emails[method](params, options);
          assert.equal(requests.length, 2);
          assert.ok(requests[0].headers["idempotency-key"]);
          assert.equal(
            requests[0].headers["idempotency-key"],
            requests[1].headers["idempotency-key"],
          );
          assert.equal(requests[0].raw, requests[1].raw);
        },
      );
  }
});
test("network disconnect retries reads and keyed sends, never unkeyed sends", async () => {
  for (const operation of ["read", "send", "keyed", "batch"])
    await mock(
      (req, res, n) => {
        if (n === 1) res.destroy();
        else json(res, { ok: true });
      },
      async (client, requests) => {
        const promise =
          operation === "read"
            ? client.domains.list()
            : operation === "batch"
              ? client.emails.batch([email], { retries: 1 })
              : client.emails.send(
                  email,
                  operation === "keyed" ? { idempotencyKey: "safe" } : {},
                );
        if (operation === "send")
          await assert.rejects(
            promise,
            (e) => e.status === 0 && e.code === "network_error",
          );
        else await promise;
        assert.equal(requests.length, operation === "send" ? 1 : 2);
      },
    );
});
test("pagination follows next and preserves filters; iterate yields emails", async () => {
  await mock(
    (req, res, n) =>
      json(res, {
        emails: [{ id: String(n) }],
        next: n % 2 === 1 ? "next cursor" : null,
        since: "now",
      }),
    async (client, requests) => {
      const pages = [];
      for await (const page of client.emails.pages({ q: "hi", limit: 1 }))
        pages.push(page);
      assert.equal(pages.length, 2);
      assert.equal(
        new URL(requests[1].url, "http://x").searchParams.get("before"),
        "next cursor",
      );
      assert.equal(
        new URL(requests[1].url, "http://x").searchParams.get("q"),
        "hi",
      );
      const items = [];
      for await (const item of client.emails.iterate()) items.push(item.id);
      assert.deepEqual(items, ["3", "4"]);
    },
  );
});
test("environment key and CommonJS distribution", () => {
  const previous = process.env.CARRIERCROW_API_KEY;
  process.env.CARRIERCROW_API_KEY = "ccm_test_env";
  try {
    assert.ok(new CarrierCrow());
    const cjs = createRequire(import.meta.url)("../dist/cjs/index.js");
    assert.ok(new cjs.CarrierCrow());
  } finally {
    if (previous === undefined) delete process.env.CARRIERCROW_API_KEY;
    else process.env.CARRIERCROW_API_KEY = previous;
  }
});
test("shared webhook vectors", () => {
  const vectors = JSON.parse(
    readFileSync(
      new URL("../../test-vectors/webhooks.json", import.meta.url),
      "utf8",
    ),
  );
  for (const c of vectors.cases) {
    const options = {
      now: c.now ?? vectors.now,
      tolerance: c.tolerance ?? 300,
    };
    if (c.valid)
      assert.deepEqual(
        webhooks.verify(
          Buffer.from(c.body),
          c.signature,
          vectors.secret,
          options,
        ),
        JSON.parse(c.body),
        c.name,
      );
    else
      assert.throws(
        () =>
          webhooks.verify(
            Buffer.from(c.body),
            c.signature,
            vectors.secret,
            options,
          ),
        CarrierCrowError,
        c.name,
      );
  }
});
test("raw UTF-8 bytes, rotation, timestamp limits and invalid signed JSON", () => {
  const secret = "whsec_test_whole-secret",
    now = 1700000000,
    body = ' {"webhook":"w","delivery":"d","events":[],"label":"☃"}\n';
  const sign = (text, t = now) =>
    `t=${t},v1=${createHmac("sha256", secret).update(`${t}.`).update(text).digest("hex")}`;
  assert.equal(
    webhooks.verify(
      Buffer.from(body),
      `${sign(body)},v1=${"0".repeat(64)}`,
      secret,
      { now },
    ).webhook,
    "w",
  );
  assert.throws(
    () => webhooks.verify(body.trim(), sign(body), secret, { now }),
    CarrierCrowError,
  );
  assert.ok(webhooks.verify(body, sign(body, now - 300), secret, { now }));
  assert.throws(
    () => webhooks.verify(body, sign(body, now + 301), secret, { now }),
    CarrierCrowError,
  );
  assert.throws(
    () => webhooks.verify("{", sign("{"), secret, { now }),
    (e) => e.code === "invalid_webhook",
  );
  for (const signature of [
    "",
    "t=x,v1=00",
    `t=${now},v1=abc`,
    `t=${now},t=${now},v1=${"a".repeat(64)}`,
  ])
    assert.throws(
      () => webhooks.verify(body, signature, secret, { now }),
      CarrierCrowError,
    );
});
test("timeout is typed and retries are bounded; malformed success is typed", async () => {
  await mock(
    (req, res) => {},
    async (client, requests) => {
      const short = new CarrierCrow("ccm_test", {
        baseUrl: client.baseUrl,
        timeout: 10,
        retryDelay: 0,
        maxRetries: 1,
      });
      await assert.rejects(
        short.domains.list(),
        (e) => e instanceof CarrierCrowError && e.code === "network_error",
      );
      assert.equal(requests.length, 2);
    },
  );
  await mock(
    (req, res) => {
      res.writeHead(200);
      res.end("not json");
    },
    async (client, requests) => {
      await assert.rejects(
        client.domains.list(),
        (e) => e.code === "invalid_response" && e.status === 200,
      );
      assert.equal(requests.length, 1);
    },
  );
});
