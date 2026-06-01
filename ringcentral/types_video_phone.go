package ringcentral

// CreateVideoBridgeRequest creates a RingCentral Video meeting bridge.
type CreateVideoBridgeRequest struct {
	Name        string                 `json:"name"`
	Type        string                 `json:"type,omitempty"`
	Security    *VideoBridgeSecurity   `json:"security,omitempty"`
	Preferences *VideoBridgePreference `json:"preferences,omitempty"`
}

// UpdateVideoBridgeRequest updates a RingCentral Video meeting bridge.
type UpdateVideoBridgeRequest struct {
	Name        string                 `json:"name,omitempty"`
	Type        string                 `json:"type,omitempty"`
	Security    *VideoBridgeSecurity   `json:"security,omitempty"`
	Preferences *VideoBridgePreference `json:"preferences,omitempty"`
}

// VideoBridgeSecurity controls meeting admission security.
type VideoBridgeSecurity struct {
	PasswordProtected bool   `json:"passwordProtected,omitempty"`
	Password          string `json:"password,omitempty"`
	NoGuests          bool   `json:"noGuests,omitempty"`
	SameAccount       bool   `json:"sameAccount,omitempty"`
	E2EE              bool   `json:"e2ee,omitempty"`
}

// VideoBridgePreference controls meeting behavior.
type VideoBridgePreference struct {
	Join               *VideoBridgeJoinPreference `json:"join,omitempty"`
	PlayTones          string                     `json:"playTones,omitempty"`
	MusicOnHold        *bool                      `json:"musicOnHold,omitempty"`
	JoinBeforeHost     *bool                      `json:"joinBeforeHost,omitempty"`
	ScreenSharing      *bool                      `json:"screenSharing,omitempty"`
	RecordingsMode     string                     `json:"recordingsMode,omitempty"`
	TranscriptionsMode string                     `json:"transcriptionsMode,omitempty"`
}

// VideoBridgeJoinPreference controls default join settings.
type VideoBridgeJoinPreference struct {
	AudioMuted          *bool                      `json:"audioMuted,omitempty"`
	VideoMuted          *bool                      `json:"videoMuted,omitempty"`
	WaitingRoomRequired string                     `json:"waitingRoomRequired,omitempty"`
	PSTN                *VideoBridgePSTNPreference `json:"pstn,omitempty"`
}

// VideoBridgePSTNPreference controls PSTN join prompts.
type VideoBridgePSTNPreference struct {
	PromptAnnouncement bool `json:"promptAnnouncement,omitempty"`
	PromptParticipants bool `json:"promptParticipants,omitempty"`
}

// VideoBridge is the RingCentral Video bridge response.
type VideoBridge struct {
	ID         string               `json:"id"`
	Name       string               `json:"name"`
	Type       string               `json:"type"`
	Discovery  VideoBridgeDiscovery `json:"discovery"`
	Security   VideoBridgeSecurity  `json:"security,omitempty"`
	CreateTime string               `json:"createTime,omitempty"`
	UpdateTime string               `json:"updateTime,omitempty"`
}

// VideoBridgeList is the response for listing RingCentral Video bridges.
type VideoBridgeList struct {
	Records []VideoBridge `json:"records"`
}

// VideoBridgeDiscovery contains join and dial-in details.
type VideoBridgeDiscovery struct {
	Web  string                 `json:"web"`
	PSTN map[string]interface{} `json:"pstn,omitempty"`
}

// VideoMeetingHistoryOptions filters RingCentral Video meeting history for
// the authenticated owner extension.
type VideoMeetingHistoryOptions struct {
	Type      string
	PerPage   int
	PageToken string
}

// VideoMeetingHistoryList is the RingCentral Video history response.
type VideoMeetingHistoryList struct {
	Meetings []VideoMeetingHistory `json:"meetings"`
	Paging   struct {
		CurrentPageToken string `json:"currentPageToken,omitempty"`
		NextPageToken    string `json:"nextPageToken,omitempty"`
	} `json:"paging"`
}

// VideoMeetingHistory is a past RingCentral Video meeting record.
type VideoMeetingHistory struct {
	ID           string                    `json:"id"`
	BridgeID     string                    `json:"bridgeId,omitempty"`
	ShortID      string                    `json:"shortId,omitempty"`
	StartTime    string                    `json:"startTime,omitempty"`
	DisplayName  string                    `json:"displayName,omitempty"`
	Type         string                    `json:"type,omitempty"`
	Status       string                    `json:"status,omitempty"`
	Duration     int                       `json:"duration,omitempty"`
	HostInfo     VideoMeetingParticipant   `json:"hostInfo,omitempty"`
	Participants []VideoMeetingParticipant `json:"participants,omitempty"`
	Recordings   []VideoMeetingRecording   `json:"recordings,omitempty"`
}

// VideoMeetingParticipant describes a meeting host or attendee.
type VideoMeetingParticipant struct {
	Type        string `json:"type,omitempty"`
	ID          string `json:"id,omitempty"`
	AccountID   string `json:"accountId,omitempty"`
	ExtensionID string `json:"extensionId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
}

// VideoMeetingRecording describes a recording attached to a meeting history item.
type VideoMeetingRecording struct {
	ID                 string `json:"id,omitempty"`
	AvailabilityStatus string `json:"availabilityStatus,omitempty"`
	StartTime          any    `json:"startTime,omitempty"`
	URL                string `json:"url,omitempty"`
	MediaURL           string `json:"mediaURL,omitempty"`
	Status             string `json:"status,omitempty"`
}

// CloudCalendarList is the connected cloud-calendar response used by FIJI's
// Calendar module for upcoming meetings.
type CloudCalendarList struct {
	Records []CloudCalendar `json:"records"`
}

// CloudCalendar identifies a connected external calendar.
type CloudCalendar struct {
	ID         string `json:"id,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
	CalendarID string `json:"calendarId,omitempty"`
	Name       string `json:"name,omitempty"`
	Connected  bool   `json:"connected,omitempty"`
	Primary    bool   `json:"primary,omitempty"`
}

// CloudCalendarEventOptions filters FIJI-compatible cloud-calendar events.
type CloudCalendarEventOptions struct {
	StartTimeFrom      string
	StartTimeTo        string
	IncludeNonRCEvents bool
	PerPage            int
	PageToken          string
}

// CloudCalendarEventList is the event list response for one cloud calendar.
type CloudCalendarEventList struct {
	Records []CloudCalendarEvent `json:"records"`
	Paging  struct {
		NextPageToken string `json:"nextPageToken,omitempty"`
	} `json:"paging"`
	SyncToken string `json:"syncToken,omitempty"`
}

// CloudCalendarEvent is a cloud calendar event returned by the same endpoint
// used by FIJI's Calendar module.
type CloudCalendarEvent struct {
	ID          string                 `json:"id"`
	Subject     string                 `json:"subject,omitempty"`
	Location    string                 `json:"location,omitempty"`
	Description string                 `json:"description,omitempty"`
	Start       CloudCalendarEventTime `json:"start,omitempty"`
	End         CloudCalendarEventTime `json:"end,omitempty"`
	StartTime   string                 `json:"startTime,omitempty"`
	EndTime     string                 `json:"endTime,omitempty"`
	Organizer   CloudCalendarPerson    `json:"organizer,omitempty"`
	Attendees   []CloudCalendarPerson  `json:"attendees,omitempty"`
	Type        string                 `json:"type,omitempty"`
	Cancelled   bool                   `json:"cancelled,omitempty"`
	IsCancelled bool                   `json:"isCancelled,omitempty"`
	WebViewURI  string                 `json:"webViewUri,omitempty"`
	WebEditURI  string                 `json:"webEditUri,omitempty"`
}

type CloudCalendarEventTime struct {
	DateTime string `json:"dateTime,omitempty"`
	TimeZone string `json:"timeZone,omitempty"`
}

type CloudCalendarPerson struct {
	Email          string `json:"email,omitempty"`
	Name           string `json:"name,omitempty"`
	Self           bool   `json:"self,omitempty"`
	ResponseStatus string `json:"responseStatus,omitempty"`
	Optional       bool   `json:"optional,omitempty"`
}

// PhoneNumberRef identifies a phone endpoint.
type PhoneNumberRef struct {
	PhoneNumber string `json:"phoneNumber,omitempty"`
	ExtensionID string `json:"extensionId,omitempty"`
}

// ExtensionPhoneNumberList is the extension phone-number list response.
type ExtensionPhoneNumberList struct {
	Records []ExtensionPhoneNumber `json:"records"`
}

// ExtensionPhoneNumber describes one phone number assigned to the current
// extension. RingOut accepts a reachable direct/callback number in from.
type ExtensionPhoneNumber struct {
	ID          any    `json:"id,omitempty"`
	PhoneNumber string `json:"phoneNumber,omitempty"`
	PaymentType string `json:"paymentType,omitempty"`
	Type        string `json:"type,omitempty"`
	UsageType   string `json:"usageType,omitempty"`
	Status      string `json:"status,omitempty"`
}

// ForwardingNumberList is the extension forwarding-number response. RingOut
// uses forwarding/callback numbers for the first leg of the call.
type ForwardingNumberList struct {
	Records []ForwardingNumber `json:"records"`
}

// ForwardingNumber describes a callback-capable number configured for the
// current extension.
type ForwardingNumber struct {
	ID          any      `json:"id,omitempty"`
	PhoneNumber string   `json:"phoneNumber,omitempty"`
	Label       string   `json:"label,omitempty"`
	Type        string   `json:"type,omitempty"`
	Features    []string `json:"features,omitempty"`
	Status      string   `json:"status,omitempty"`
	Enabled     bool     `json:"enabled,omitempty"`
	Hidden      bool     `json:"hidden,omitempty"`
	FlipNumber  string   `json:"flipNumber,omitempty"`
}

// CreateRingOutRequest creates a two-legged RingOut call.
type CreateRingOutRequest struct {
	From       *PhoneNumberRef `json:"from,omitempty"`
	To         PhoneNumberRef  `json:"to"`
	CallerID   *PhoneNumberRef `json:"callerId,omitempty"`
	PlayPrompt bool            `json:"playPrompt,omitempty"`
}

// RingOut is the RingOut session response.
type RingOut struct {
	ID     string        `json:"id"`
	URI    string        `json:"uri,omitempty"`
	Status RingOutStatus `json:"status"`
}

// RingOutStatus contains combined and per-leg RingOut statuses.
type RingOutStatus struct {
	CallStatus   string `json:"callStatus"`
	CallerStatus string `json:"callerStatus"`
	CalleeStatus string `json:"calleeStatus"`
}

// CallLogOptions filters extension-level or account-level call logs.
type CallLogOptions struct {
	RecordCount int
	Page        int
	PageToken   string
	ExtensionID string
	View        string
	Direction   string
	Type        string
	Result      string
	DateFrom    string
	DateTo      string
}

// CallLogList is the extension call log list response.
type CallLogList struct {
	Records    []CallLogRecord `json:"records"`
	Navigation struct {
		NextPage struct {
			URI string `json:"uri"`
		} `json:"nextPage"`
		NextPageToken string `json:"nextPageToken"`
	} `json:"navigation"`
	Paging struct {
		Page          int `json:"page"`
		TotalPages    int `json:"totalPages"`
		PerPage       int `json:"perPage"`
		TotalElements int `json:"totalElements"`
	} `json:"paging"`
}

// CallLogRecord is a single RingCentral call log record.
type CallLogRecord struct {
	URI                string          `json:"uri,omitempty"`
	ID                 string          `json:"id"`
	SessionID          string          `json:"sessionId"`
	TelephonySessionID string          `json:"telephonySessionId,omitempty"`
	StartTime          string          `json:"startTime"`
	Duration           int             `json:"duration"`
	Type               string          `json:"type"`
	Direction          string          `json:"direction"`
	Action             string          `json:"action"`
	Result             string          `json:"result"`
	To                 CallLogParty    `json:"to"`
	From               CallLogParty    `json:"from"`
	Transport          string          `json:"transport,omitempty"`
	LastModifiedTime   string          `json:"lastModifiedTime,omitempty"`
	Legs               []CallLogRecord `json:"legs,omitempty"`
}

// CallLogParty is a participant in a call log record.
type CallLogParty struct {
	PhoneNumber string `json:"phoneNumber,omitempty"`
	Name        string `json:"name,omitempty"`
	Location    string `json:"location,omitempty"`
}
