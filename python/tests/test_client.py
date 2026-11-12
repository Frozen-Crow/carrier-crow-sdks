import json
import socket
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import parse_qs, urlsplit

import pytest

from carriercrow import CarrierCrow, CarrierCrowError, webhooks


@pytest.fixture
def server():
    requests = []
    replies = []

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self):
            self.handle_request()

        def do_POST(self):
            self.handle_request()

        def handle_request(self):
            raw = self.rfile.read(int(self.headers.get("Content-Length", 0)))
            requests.append(
                (
                    self.command,
                    self.path,
                    self.headers,
                    json.loads(raw) if raw else None,
                )
            )
            status, payload, headers = replies.pop(0)
            if status == 0:
                self.connection.shutdown(socket.SHUT_RDWR)
                self.connection.close()
                return
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            for key, value in headers.items():
                self.send_header(key, value)
            self.end_headers()
            self.wfile.write(json.dumps(payload).encode())

        def log_message(self, *args):
            pass

    httpd = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()
    with CarrierCrow(
        "ccm_test_key",
        base_url=f"http://127.0.0.1:{httpd.server_port}",
        backoff=0,
        sleep=lambda delay: None,
    ) as client:
        yield client, requests, replies
    httpd.shutdown()
    thread.join()
    httpd.server_close()


SEND = {
    "from": "a@example.com",
    "to": ["b@example.com"],
    "subject": "Test",
    "text": "Hello",
    "reply_to": "c@example.com",
    "message_id": "test-1",
    "attachments": [
        {"filename": "test.txt", "content": "aGk=", "content_type": "text/plain"}
    ],
}


def test_every_request(server):
    client, requests, replies = server
    results = [
        {"status": "accepted", "message_id": "test-1", "recipients": []},
        {"results": []},
        {"email": {"messageId": "test-1"}, "events": [], "updates": []},
        {"emails": [], "next": None, "since": "now"},
        {"domains": []},
        {"id": "domain/one"},
        {"id": "domain/one", "checked_at": "now"},
    ]
    replies.extend((200, result, {}) for result in results)
    assert client.emails.send(SEND, idempotency_key="test-key") == results[0]
    assert client.emails.batch([SEND], idempotency_key="batch-key") == results[1]
    assert client.emails.get("a/b") == results[2]
    query = {
        "q": "a@b",
        "status": ["delivered", "bounced"],
        "tenant": "one",
        "app": "shop",
        "days": 3,
        "before": "cursor",
        "limit": 2,
    }
    assert client.emails.list(query) == results[3]
    assert client.domains.list() == results[4]
    assert client.domains.get("domain/one") == results[5]
    assert client.domains.check("domain/one") == results[6]
    assert [(m, urlsplit(p).path) for m, p, _, _ in requests] == [
        ("POST", "/v1/emails"),
        ("POST", "/v1/emails/batch"),
        ("GET", "/v1/emails/a%2Fb"),
        ("GET", "/v1/emails"),
        ("GET", "/v1/domains"),
        ("GET", "/v1/domains/domain%2Fone"),
        ("POST", "/v1/domains/domain%2Fone/check"),
    ]
    for _, _, headers, _ in requests:
        assert headers["Authorization"] == "Bearer ccm_test_key"
        assert headers["User-Agent"] == "carriercrow-python/0.1.0"
    assert requests[0][2]["Idempotency-Key"] == "test-key"
    assert requests[0][2]["Content-Type"] == "application/json"
    assert requests[0][3] == SEND
    assert requests[1][3] == {"emails": [SEND]}
    assert parse_qs(urlsplit(requests[3][1]).query) == {
        key: value if isinstance(value, list) else [str(value)]
        for key, value in query.items()
    }
    assert requests[-1][3] is None


def test_error_mapping(server):
    client, requests, replies = server
    replies.append(
        (
            400,
            {"error": "Fix recipient", "code": "invalid_request", "field": "to[2]"},
            {"Retry-After": "7"},
        )
    )
    with pytest.raises(CarrierCrowError) as caught:
        client.emails.send(SEND)
    error = caught.value
    assert (
        error.status,
        error.code,
        error.message,
        error.field,
        error.retry_after,
        error.retryAfter,
    ) == (400, "invalid_request", "Fix recipient", "to[2]", 7, 7)
    assert len(requests) == 1


@pytest.mark.parametrize("status", [429, 503, 0])
@pytest.mark.parametrize("method", ["send", "batch"])
def test_safe_send_retries(server, status, method):
    client, requests, replies = server
    call = getattr(client.emails, method)
    body = SEND if method == "send" else [SEND]
    replies.extend(
        [
            (status, {"error": "Wait", "code": "unavailable"}, {"Retry-After": "2"}),
            (200, {"ok": True}, {}),
        ]
    )
    delays = []
    client._sleep = delays.append
    assert call(body, retry=True) == {"ok": True}
    assert len(requests) == 2
    assert requests[0][2]["Idempotency-Key"] == requests[1][2]["Idempotency-Key"]
    assert requests[0][3] == requests[1][3]
    assert delays == ([0] if status == 0 else [2])


@pytest.mark.parametrize("status", [429, 503, 0])
@pytest.mark.parametrize("method", ["send", "batch"])
def test_send_without_key_never_retries(server, status, method):
    client, requests, replies = server
    replies.append((status, {"error": "Wait", "code": "unavailable"}, {}))
    with pytest.raises(CarrierCrowError):
        getattr(client.emails, method)(SEND if method == "send" else [SEND])
    assert len(requests) == 1


def test_read_retries_and_exhaustion(server):
    client, requests, replies = server
    replies.extend([(503, {"error": "Wait", "code": "unavailable"}, {})] * 3)
    with pytest.raises(CarrierCrowError) as caught:
        client.domains.list()
    assert caught.value.status == 503
    assert len(requests) == 3
    replies.extend([(0, {}, {}), (200, {"domains": []}, {})])
    assert client.domains.list() == {"domains": []}


def test_paging(server):
    client, requests, replies = server
    replies.extend(
        [
            (200, {"emails": [{"id": "1"}], "next": "cursor+next", "since": "now"}, {}),
            (200, {"emails": [{"id": "2"}], "next": None, "since": "now"}, {}),
        ]
    )
    query = {"status": ["delivered"], "limit": 1}
    assert list(client.emails.iter(query)) == [{"id": "1"}, {"id": "2"}]
    assert parse_qs(urlsplit(requests[1][1]).query) == {
        "status": ["delivered"],
        "limit": ["1"],
        "before": ["cursor+next"],
    }
    assert "before" not in query


def test_env_and_defaults(monkeypatch):
    monkeypatch.setenv("CARRIERCROW_API_KEY", "ccm_test_env")
    with CarrierCrow() as client:
        assert client.api_key == "ccm_test_env"
        assert client.base_url == "https://api.mta.carriercrow.com"


def test_webhook_vectors():
    vectors = json.loads(
        (Path(__file__).parents[2] / "test-vectors/webhooks.json").read_text()
    )
    for case in vectors["cases"]:
        args = (case["body"].encode(), case["signature"], vectors["secret"])
        kwargs = {
            "now": case.get("now", vectors["now"]),
            "tolerance": case.get("tolerance", 300),
        }
        if case["valid"]:
            assert webhooks.verify(*args, **kwargs) == json.loads(case["body"]), case[
                "name"
            ]
        else:
            with pytest.raises(CarrierCrowError):
                webhooks.verify(*args, **kwargs)
