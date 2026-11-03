package catalog

import "fmt"

// Service is a privacy permission as System Settings names it, with the
// id macOS's privacy database records it under.
type Service struct {
	Name   string
	TCC    string
	anchor string
}

var services = []Service{
	{"Accessibility", "kTCCServiceAccessibility", "Privacy_Accessibility"},
	{"Input Monitoring", "kTCCServiceListenEvent", "Privacy_ListenEvent"},
	{"Screen Recording", "kTCCServiceScreenCapture", "Privacy_ScreenCapture"},
	{"Full Disk Access", "kTCCServiceSystemPolicyAllFiles", "Privacy_AllFiles"},
	{"Automation", "kTCCServiceAppleEvents", "Privacy_Automation"},
	{"Camera", "kTCCServiceCamera", "Privacy_Camera"},
	{"Microphone", "kTCCServiceMicrophone", "Privacy_Microphone"},
	{"Developer Tools", "kTCCServiceDeveloperTool", "Privacy_DevTools"},
	{"Calendars", "kTCCServiceCalendar", "Privacy_Calendars"},
	{"Contacts", "kTCCServiceAddressBook", "Privacy_Contacts"},
	{"Photos", "kTCCServicePhotos", "Privacy_Photos"},
	{"Reminders", "kTCCServiceReminders", "Privacy_Reminders"},
	{"Bluetooth", "kTCCServiceBluetoothAlways", "Privacy_Bluetooth"},
}

// linkFormats holds, per macOS major, the System Settings URL a privacy
// pane's anchor completes. Each is checked by hand before a release (spec
// 12); a major without one gets no link, only the pane's name.
var linkFormats = map[string]string{
	"26": "x-apple.systempreferences:com.apple.settings.PrivacySecurity.extension?%s",
}

var loginItemsLinks = map[string]string{
	"26": "x-apple.systempreferences:com.apple.LoginItems-Settings.extension",
}

func ServiceNamed(name string) (Service, bool) {
	for _, s := range services {
		if s.Name == name {
			return s, true
		}
	}
	return Service{}, false
}

func ServiceForTCC(id string) (Service, bool) {
	for _, s := range services {
		if s.TCC == id {
			return s, true
		}
	}
	return Service{}, false
}

func (s Service) Where() string { return "System Settings > Privacy & Security > " + s.Name }

// Link is the deep link to s's pane on macOS major, or "".
func (s Service) Link(major string) string {
	format, ok := linkFormats[major]
	if !ok {
		return ""
	}
	return fmt.Sprintf(format, s.anchor)
}

// LoginItemsLink is the deep link to Login Items on macOS major, or "".
func LoginItemsLink(major string) string { return loginItemsLinks[major] }
