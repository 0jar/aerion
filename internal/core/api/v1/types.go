package v1

import "time"

// Address represents an email participant.
type Address struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

// Attachment represents file content attached to a message or compose request.
// Either Data or Path must be set; Data takes precedence when both are present.
type Attachment struct {
	Filename string `json:"filename"`
	MIMEType string `json:"mimeType"`
	Size     int64  `json:"size"`
	Data     []byte `json:"data,omitempty"`
	Path     string `json:"path,omitempty"`
	IsInline bool   `json:"isInline,omitempty"`
	ContentID string `json:"contentId,omitempty"`
}

// MessageRef identifies a message in storage. Used by ComposeRequest for replies.
type MessageRef struct {
	AccountID string `json:"accountId"`
	FolderID  string `json:"folderId"`
	MessageID string `json:"messageId"` // Aerion DB id (not RFC 5322 Message-ID)
}

// Flags is the set of standard IMAP flag states for a message.
type Flags struct {
	Seen      bool `json:"seen"`
	Flagged   bool `json:"flagged"`
	Answered  bool `json:"answered"`
	Draft     bool `json:"draft"`
	Deleted   bool `json:"deleted"`
	Forwarded bool `json:"forwarded"`
}

// FolderKind classifies special folders by purpose.
type FolderKind string

const (
	FolderKindInbox   FolderKind = "inbox"
	FolderKindSent    FolderKind = "sent"
	FolderKindDrafts  FolderKind = "drafts"
	FolderKindTrash   FolderKind = "trash"
	FolderKindArchive FolderKind = "archive"
	FolderKindSpam    FolderKind = "spam"
	FolderKindAll     FolderKind = "all"
	FolderKindStarred FolderKind = "starred"
)

// Message is the API-surface representation of an email message. It mirrors
// internal/message.Message but is decoupled from core's storage shape so core
// can evolve without breaking the extension API.
type Message struct {
	ID         string    `json:"id"`
	AccountID  string    `json:"accountId"`
	FolderID   string    `json:"folderId"`
	UID        uint32    `json:"uid"`
	MessageID  string    `json:"messageId"`  // RFC 5322 Message-ID
	InReplyTo  string    `json:"inReplyTo"`
	References []string  `json:"references"`
	ThreadID   string    `json:"threadId"`
	Subject    string    `json:"subject"`
	From       Address   `json:"from"`
	To         []Address `json:"to"`
	Cc         []Address `json:"cc"`
	Bcc        []Address `json:"bcc"`
	ReplyTo    string    `json:"replyTo"`
	Date       time.Time `json:"date"`
	BodyHTML   string    `json:"bodyHtml,omitempty"`
	BodyText   string    `json:"bodyText,omitempty"`
	Snippet    string    `json:"snippet"`
	Flags      Flags     `json:"flags"`
	Size       int       `json:"size"`
	HasAttachments bool  `json:"hasAttachments"`
}

// Folder is the API-surface representation of a mail folder.
type Folder struct {
	ID          string     `json:"id"`
	AccountID   string     `json:"accountId"`
	Name        string     `json:"name"`
	Path        string     `json:"path"`
	Kind        FolderKind `json:"kind"`
	ParentID    string     `json:"parentId,omitempty"`
	Subscribed  bool       `json:"subscribed"`
	TotalCount  int        `json:"totalCount"`
	UnreadCount int        `json:"unreadCount"`
}

// MessageFilter is the input to Mail.ListMessages. Zero-valued fields are
// not applied as filters.
type MessageFilter struct {
	AccountID string    `json:"accountId,omitempty"`
	FolderID  string    `json:"folderId,omitempty"`
	Unread    *bool     `json:"unread,omitempty"`
	Starred   *bool     `json:"starred,omitempty"`
	From      string    `json:"from,omitempty"`
	Since     time.Time `json:"since,omitempty"`
	Limit     int       `json:"limit,omitempty"`
	Offset    int       `json:"offset,omitempty"`
}

// Contact is the API-surface representation of a contact.
type Contact struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Emails    []string `json:"emails"`
	SourceID  string   `json:"sourceId,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ContactFilter is the input to Contacts.ListContacts.
type ContactFilter struct {
	Query    string `json:"query,omitempty"`
	SourceID string `json:"sourceId,omitempty"`
	Limit    int    `json:"limit,omitempty"`
	Offset   int    `json:"offset,omitempty"`
}

// Unregister is returned from UI/Event registration calls. Callers invoke
// it to remove the registration (e.g., on extension disable or shutdown).
type Unregister func()

// Unsubscribe is the cancel function returned by event subscriptions.
type Unsubscribe func()
