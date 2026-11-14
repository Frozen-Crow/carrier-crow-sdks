package carriercrow

import "encoding/json"

// Addresses accepts either a JSON string or array of strings.
type Addresses []string

func (a *Addresses) UnmarshalJSON(b []byte) error {
	var s string
	if json.Unmarshal(b, &s) == nil {
		*a = Addresses{s}
		return nil
	}
	var ss []string
	if err := json.Unmarshal(b, &ss); err != nil {
		return err
	}
	*a = ss
	return nil
}

type Attachment struct {
	Filename    string `json:"filename"`
	Content     string `json:"content"`
	ContentType string `json:"content_type,omitempty"`
}
type SendEmail struct {
	From        string            `json:"from"`
	To          Addresses         `json:"to"`
	CC          Addresses         `json:"cc,omitempty"`
	BCC         Addresses         `json:"bcc,omitempty"`
	ReplyTo     Addresses         `json:"reply_to,omitempty"`
	Subject     string            `json:"subject"`
	HTML        string            `json:"html,omitempty"`
	Text        string            `json:"text,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Attachments []Attachment      `json:"attachments,omitempty"`
	MessageID   string            `json:"message_id,omitempty"`
	Subscriber  string            `json:"subscriber,omitempty"`
	Campaign    string            `json:"campaign,omitempty"`
	Stream      string            `json:"stream,omitempty"`
	App         string            `json:"app,omitempty"`
}
type Recipient struct {
	Address string  `json:"address"`
	ID      *string `json:"id"`
}
type SendResult struct {
	Object     string      `json:"object"`
	Status     string      `json:"status"`
	MessageID  *string     `json:"message_id"`
	Recipients []Recipient `json:"recipients"`
}
type BatchResult struct {
	Index      int         `json:"index"`
	Status     string      `json:"status"`
	MessageID  *string     `json:"message_id"`
	Recipients []Recipient `json:"recipients,omitempty"`
	Error      string      `json:"error,omitempty"`
	Code       string      `json:"code,omitempty"`
	Field      string      `json:"field,omitempty"`
}
type BatchResponse struct {
	Results []BatchResult `json:"results"`
}
type EmailStatus string

const (
	Delivered  EmailStatus = "delivered"
	Delayed    EmailStatus = "delayed"
	Waiting    EmailStatus = "waiting"
	Bounced    EmailStatus = "bounced"
	Expired    EmailStatus = "expired"
	Complained EmailStatus = "complained"
)

type EmailResponse struct {
	Code     int     `json:"code"`
	Enhanced *string `json:"enhanced"`
	Text     string  `json:"text"`
}
type Held struct {
	Until *string `json:"until"`
	By    string  `json:"by"`
}
type EmailSummary struct {
	ID          string         `json:"id"`
	MessageID   *string        `json:"messageId"`
	Recipient   string         `json:"recipient"`
	Tenant      string         `json:"tenant"`
	Campaign    *string        `json:"campaign"`
	App         *string        `json:"app"`
	Status      EmailStatus    `json:"status"`
	FirstAt     string         `json:"firstAt"`
	At          string         `json:"at"`
	Tries       int            `json:"tries"`
	Provider    string         `json:"provider"`
	Moved       bool           `json:"moved"`
	Reason      *string        `json:"reason"`
	BounceClass *string        `json:"bounceClass"`
	Response    *EmailResponse `json:"response"`
	Held        *Held          `json:"held"`
}
type Email struct {
	EmailSummary
	From       *string `json:"from"`
	ReturnPath *string `json:"returnPath"`
	Subscriber *string `json:"subscriber"`
}
type EmailEvent struct {
	At            string         `json:"at"`
	Kind          string         `json:"kind"`
	Try           int            `json:"try,omitempty"`
	Response      *EmailResponse `json:"response,omitempty"`
	Reason        *string        `json:"reason,omitempty"`
	BounceClass   *string        `json:"bounceClass,omitempty"`
	Source        *string        `json:"source,omitempty"`
	SourceAddress *string        `json:"sourceAddress,omitempty"`
	Server        *string        `json:"server,omitempty"`
	TLS           *string        `json:"tls,omitempty"`
	Until         *string        `json:"until,omitempty"`
}
type EmailDetail struct {
	Email   Email                        `json:"email"`
	Events  []EmailEvent                 `json:"events"`
	Updates []map[string]json.RawMessage `json:"updates"`
}
type EmailList struct {
	Emails []EmailSummary `json:"emails"`
	Next   *string        `json:"next"`
	Since  string         `json:"since"`
}
type ListQuery struct {
	Q      string
	Status []EmailStatus
	Tenant string
	App    string
	Days   int
	Before string
	Limit  int
}
type DNSRecord struct {
	For    string `json:"for"`
	Type   string `json:"type"`
	Name   string `json:"name"`
	Value  string `json:"value"`
	Status string `json:"status"`
}
type Domain struct {
	ID        string      `json:"id"`
	Name      string      `json:"name"`
	Sender    string      `json:"sender"`
	Verified  bool        `json:"verified"`
	Records   []DNSRecord `json:"records"`
	CheckedAt *string     `json:"checked_at"`
}
type DomainList struct {
	Domains []Domain `json:"domains"`
}
type WebhookBody struct {
	Webhook  string         `json:"webhook"`
	Delivery string         `json:"delivery"`
	Test     bool           `json:"test,omitempty"`
	Events   []WebhookEvent `json:"events"`
}
type WebhookMessage struct {
	ID         *string `json:"id"`
	MTAID      *string `json:"mtaId"`
	Recipient  string  `json:"recipient"`
	Subscriber *string `json:"subscriber"`
	Campaign   *string `json:"campaign"`
}
type Bounce struct {
	Class *string `json:"class"`
	Kind  string  `json:"kind"`
}
type Complaint struct {
	Type     *string `json:"type"`
	Reporter *string `json:"reporter"`
}
type WebhookEvent struct {
	ID        string         `json:"id"`
	Type      string         `json:"type"`
	At        string         `json:"at"`
	Tenant    string         `json:"tenant"`
	Message   WebhookMessage `json:"message"`
	Attempts  int            `json:"attempts,omitempty"`
	Response  *EmailResponse `json:"response,omitempty"`
	Bounce    *Bounce        `json:"bounce,omitempty"`
	Complaint *Complaint     `json:"complaint,omitempty"`
}
