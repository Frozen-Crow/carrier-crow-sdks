"""Typed wire models. JSON keys intentionally match the public API exactly."""

from __future__ import annotations

from typing import Any, Dict, List, Literal, Optional, TypedDict, Union

Addresses = Union[str, List[str]]
EmailStatus = Literal[
    "delivered", "delayed", "waiting", "bounced", "expired", "complained"
]


class AttachmentRequired(TypedDict):
    filename: str
    content: str


class Attachment(AttachmentRequired, total=False):
    content_type: str


SendRequired = TypedDict("SendRequired", {"from": str, "to": Addresses, "subject": str})


class SendEmail(SendRequired, total=False):
    cc: Addresses
    bcc: Addresses
    reply_to: Addresses
    html: str
    text: str
    headers: Dict[str, str]
    attachments: List[Attachment]
    message_id: str
    subscriber: str
    campaign: str
    app: str


class Recipient(TypedDict):
    address: str
    id: str


class SendResult(TypedDict):
    object: Literal["email"]
    status: Literal["accepted"]
    message_id: Optional[str]
    recipients: List[Recipient]


class EmailResponse(TypedDict):
    code: int
    enhanced: Optional[str]
    text: str


class Held(TypedDict, total=False):
    until: Optional[str]
    by: Literal["pause", "limit", "no-source", "other"]


class EmailSummary(TypedDict):
    id: str
    messageId: Optional[str]
    recipient: str
    tenant: str
    campaign: Optional[str]
    app: Optional[str]
    status: EmailStatus
    firstAt: str
    at: str
    tries: int
    provider: str
    moved: bool
    reason: Optional[str]
    bounceClass: Optional[str]
    response: Optional[EmailResponse]
    held: Optional[Held]


EmailExtras = TypedDict(
    "EmailExtras",
    {"from": Optional[str], "returnPath": Optional[str], "subscriber": Optional[str]},
    total=False,
)


class Email(EmailSummary, EmailExtras):
    pass


class EmailEventRequired(TypedDict):
    at: str
    kind: Literal[
        "received",
        "held",
        "delayed",
        "delivered",
        "bounced",
        "removed",
        "expired",
        "complained",
    ]


EmailEventOptional = TypedDict(
    "EmailEventOptional",
    {
        "try": int,
        "response": EmailResponse,
        "reason": Optional[str],
        "bounceClass": Optional[str],
        "source": Optional[str],
        "sourceAddress": Optional[str],
        "server": Optional[str],
        "tls": Optional[str],
        "until": Optional[str],
    },
    total=False,
)


class EmailEvent(EmailEventRequired, EmailEventOptional):
    pass


class EmailDetail(TypedDict):
    email: Email
    events: List[EmailEvent]
    updates: List[Dict[str, Any]]


class EmailList(TypedDict):
    emails: List[EmailSummary]
    next: Optional[str]
    since: str


DnsRecord = TypedDict(
    "DnsRecord",
    {
        "for": Literal["signature", "permission", "fakes"],
        "type": str,
        "name": str,
        "value": str,
        "status": Literal["found", "missing", "wrong"],
    },
)


class Domain(TypedDict):
    id: str
    name: str
    sender: str
    verified: bool
    records: List[DnsRecord]
    checked_at: Optional[str]


class WebhookMessage(TypedDict):
    id: Optional[str]
    mtaId: Optional[str]
    recipient: str
    subscriber: Optional[str]
    campaign: Optional[str]


Bounce = TypedDict(
    "Bounce",
    {"class": Optional[str], "kind": Literal["remote", "admin", "report"]},
    total=False,
)
Complaint = TypedDict(
    "Complaint", {"type": Optional[str], "reporter": Optional[str]}, total=False
)


class WebhookEventRequired(TypedDict):
    id: str
    type: Literal[
        "message.received",
        "message.delivered",
        "message.deferred",
        "message.bounced",
        "message.expired",
        "message.complained",
    ]
    at: str
    tenant: str
    message: WebhookMessage


class WebhookEvent(WebhookEventRequired, total=False):
    attempts: int
    response: EmailResponse
    bounce: Bounce
    complaint: Complaint


class WebhookBodyRequired(TypedDict):
    webhook: str
    delivery: str
    events: List[WebhookEvent]


class WebhookBody(WebhookBodyRequired, total=False):
    test: bool


class BatchResultRequired(TypedDict):
    index: int
    status: Literal["accepted", "rejected"]


class BatchResult(BatchResultRequired, total=False):
    message_id: Optional[str]
    recipients: List[Recipient]
    error: str
    code: str
    field: str


class BatchResponse(TypedDict):
    results: List[BatchResult]


class EmailQuery(TypedDict, total=False):
    q: str
    status: List[EmailStatus]
    tenant: str
    app: str
    days: int
    before: str
    limit: int
