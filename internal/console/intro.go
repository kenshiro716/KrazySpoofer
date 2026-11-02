package console

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"krazyspoofer/internal/color"
)

const introSubtitle = "Asset Reuploader  -  Uploaderv3Newest"

var introLines = []string{"MADE BY", "RANJI LMAO"}

var introGlyphs = map[rune][5]string{
	'M': {"#   #", "## ##", "# # #", "#   #", "#   #"},
	'A': {" ### ", "#   #", "#####", "#   #", "#   #"},
	'D': {"#### ", "#   #", "#   #", "#   #", "#### "},
	'E': {"#####", "#    ", "#### ", "#    ", "#####"},
	'B': {"#### ", "#   #", "#### ", "#   #", "#### "},
	'Y': {"#   #", " # # ", "  #  ", "  #  ", "  #  "},
	'R': {"#### ", "#   #", "#### ", "#  # ", "#   #"},
	'N': {"#   #", "##  #", "# # #", "#  ##", "#   #"},
	'J': {"  ###", "   # ", "   # ", "#  # ", " ##  "},
	'I': {"#####", "  #  ", "  #  ", "  #  ", "#####"},
	'L': {"#    ", "#    ", "#    ", "#    ", "#####"},
	'O': {" ### ", "#   #", "#   #", "#   #", " ### "},
	' ': {"   ", "   ", "   ", "   ", "   "},
}

// 256-color codes forming a rainbow loop.
var introRainbow = []int{
	196, 202, 208, 214, 220, 226, 190, 154, 118, 82, 46, 47, 48, 49, 50,
	51, 45, 39, 33, 27, 21, 57, 93, 129, 165, 201, 200, 199, 198, 197,
}

// buildIntroRows turns each intro line into five rows of block text.
func buildIntroRows() [][]rune {
	rows := make([][]rune, 0, len(introLines)*6)
	for i, line := range introLines {
		if i > 0 {
			rows = append(rows, nil)
		}
		var glyphRows [5]strings.Builder
		for _, r := range line {
			g := introGlyphs[r]
			for y := range glyphRows {
				glyphRows[y].WriteString(g[y])
				glyphRows[y].WriteByte(' ')
			}
		}
		for y := range glyphRows {
			rows = append(rows, []rune(glyphRows[y].String()))
		}
	}
	return rows
}

func renderIntroFrame(rows [][]rune, visibleCols, shift int, subtitle string) string {
	var b strings.Builder
	b.WriteString("\x1b[H\n")
	for _, row := range rows {
		b.WriteString("  ")
		lastColor := -1
		for x, r := range row {
			if r != '#' || x >= visibleCols {
				if lastColor != -1 {
					b.WriteString("\x1b[0m")
					lastColor = -1
				}
				b.WriteByte(' ')
				continue
			}
			c := introRainbow[(x/2+shift)%len(introRainbow)]
			if c != lastColor {
				fmt.Fprintf(&b, "\x1b[38;5;%dm", c)
				lastColor = c
			}
			b.WriteRune('█')
		}
		b.WriteString("\x1b[0m\x1b[K\n")
	}
	b.WriteString("\n  \x1b[1;38;5;213m")
	b.WriteString(subtitle)
	b.WriteString("\x1b[0m\x1b[K\n")
	return b.String()
}

// PlayIntro shows the animated "MADE BY RANJI LMAO" banner. It does nothing
// when stdout is not an interactive console, so piped output stays clean.
func PlayIntro() {
	fd := os.Stdout.Fd()
	if !isatty.IsTerminal(fd) && !isatty.IsCygwinTerminal(fd) {
		return
	}

	out := color.Output
	rows := buildIntroRows()
	width := 0
	for _, row := range rows {
		width = max(width, len(row))
	}

	fmt.Fprint(out, "\x1b[?25l")
	defer fmt.Fprint(out, "\x1b[0m\x1b[?25h")

	shift := 0
	for cols := 0; cols <= width; cols += 2 {
		fmt.Fprint(out, renderIntroFrame(rows, cols, shift, ""))
		shift++
		time.Sleep(15 * time.Millisecond)
	}

	for i := 0; i <= len(introSubtitle); i++ {
		fmt.Fprint(out, renderIntroFrame(rows, width, shift, introSubtitle[:i]))
		shift++
		time.Sleep(20 * time.Millisecond)
	}

	for range 35 {
		fmt.Fprint(out, renderIntroFrame(rows, width, shift, introSubtitle))
		shift++
		time.Sleep(30 * time.Millisecond)
	}
}
