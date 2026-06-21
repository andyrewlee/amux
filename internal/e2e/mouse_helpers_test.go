package e2e

import (
	"fmt"

	"github.com/andyrewlee/amux/internal/ui/layout"
)

type wheelInput struct {
	outer     string
	forwarded string
}

func centerPaneLeftClickInput(width, height, termX, termY int) string {
	l := layout.NewManager()
	l.Resize(width, height)
	centerStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX()
	screenX := centerStartX + 2 + termX
	screenY := l.TopGutter() + 2 + termY
	return leftClickInput(screenX, screenY)
}

func dashboardRowLeftClickInput(width, height, screenY int) string {
	l := layout.NewManager()
	l.Resize(width, height)
	return leftClickInput(l.LeftGutter()+4, screenY)
}

func leftClickInput(screenX, screenY int) string {
	return fmt.Sprintf("\x1b[<0;%d;%dM\x1b[<0;%d;%dm", screenX+1, screenY+1, screenX+1, screenY+1)
}

func centerPaneWheelUpInput(width, height, termX, termY int) wheelInput {
	l := layout.NewManager()
	l.Resize(width, height)
	centerStartX := l.LeftGutter() + l.DashboardWidth() + l.GapX()
	screenX := centerStartX + 2 + termX
	screenY := l.TopGutter() + 2 + termY
	return wheelInput{
		outer:     fmt.Sprintf("\x1b[<64;%d;%dM", screenX+1, screenY+1),
		forwarded: fmt.Sprintf("\x1b[<64;%d;%dM", termX+1, termY+1),
	}
}
