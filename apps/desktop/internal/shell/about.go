package shell

import (
	"runtime"

	"github.com/AnoRebel/tdc/apps/desktop/internal/buildinfo"
)

/*
What this is, for somebody who needs to say what they are running.

The first thing anybody reporting a problem is asked for, and the last thing
they should have to hunt for — so it is in both the Help menu and the tray.
*/

// About is what the About screen shows.
type About struct {
	// Name as somebody would say it.
	Name string `json:"name"`
	// Version of this build, kept in step with the repository's VERSION file.
	Version string `json:"version"`
	/*
		Platform and architecture, because "it does not work on my machine" is
		answerable only when somebody knows which machine.
	*/
	Platform string `json:"platform"`
	// Description, so an About screen says what the application is for.
	Description string `json:"description"`
	// Licence, since this is free software and somebody may want to know which.
	Licence string `json:"licence"`
	// Homepage for the source, which is where a problem is reported.
	Homepage string `json:"homepage"`
}

// AboutService is the About screen's data, as the interface reads it.
type AboutService struct{}

// NewAboutService builds it.
func NewAboutService() *AboutService { return &AboutService{} }

// About reports what this build is.
func (s *AboutService) About() About {
	return About{
		Name:    "TDC",
		Version: buildinfo.Version,
		// GOOS and GOARCH as Go reports them, which is what a bug report needs
		// rather than a prettier name that has to be translated back.
		Platform:    runtime.GOOS + "/" + runtime.GOARCH,
		Description: "Chat, project tracking, and a directory connecting developers with the people who need them.",
		Licence:     "Apache-2.0",
		Homepage:    "https://github.com/AnoRebel/tdc",
	}
}
