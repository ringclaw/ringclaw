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

// VideoBridgeDiscovery contains join and dial-in details.
type VideoBridgeDiscovery struct {
	Web  string                 `json:"web"`
	PSTN map[string]interface{} `json:"pstn,omitempty"`
}

// PhoneNumberRef identifies a phone endpoint.
type PhoneNumberRef struct {
	PhoneNumber string `json:"phoneNumber,omitempty"`
	ExtensionID string `json:"extensionId,omitempty"`
}

// CreateRingOutRequest creates a two-legged RingOut call.
type CreateRingOutRequest struct {
	From       PhoneNumberRef  `json:"from"`
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

// CallLogOptions filters extension-level call logs.
type CallLogOptions struct {
	RecordCount int
	Page        int
	PageToken   string
	View        string
	Direction   string
	Type        string
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
