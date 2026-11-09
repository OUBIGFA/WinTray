package kit

import "github.com/egoist/mygo/ui"

// The type ramp: a fixed scale with a ratio of about 1.15 between steps,
// relative to the theme's FontSize (14 DIPs times Windows' text size).
// Roles, not sizes, are what pages choose.
const (
	captionSize = 12.0 / 14
	helpSize    = 13.0 / 14
	headingSize = 16.0 / 14
	titleSize   = 20.0 / 14
)

// Caption is metadata: counts, timestamps, badges' companions.
func Caption(c *ui.Context, s string) ui.Element {
	t := c.Theme()
	return ui.Text(c, s).FontSize(t.Rem(captionSize)).TextColor(t.TextMuted)
}

// Help explains a setting or a state, below or beside what it explains.
func Help(c *ui.Context, s string) ui.Element {
	t := c.Theme()
	return ui.Text(c, s).FontSize(t.Rem(helpSize)).LineHeight(1.45).TextColor(t.TextMuted)
}

// Body is ordinary text.
func Body(c *ui.Context, s string) ui.Element { return ui.Text(c, s).LineHeight(1.45) }

// Strong names a row, an item or a field.
func Strong(c *ui.Context, s string) ui.Element { return ui.Text(c, s).FontWeight(600) }

// Label names a setting row; it is ordinary weight so a page of rows does
// not shout, with the group heading carrying the emphasis.
func Label(c *ui.Context, s string) ui.Element { return ui.Text(c, s).LineHeight(1.4) }

// Heading titles a group of settings.
func Heading(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(c.Theme().Rem(headingSize)).FontWeight(600).LineHeight(1.35)
}

// Title is the largest role: a dialog's or an empty state's title.
func Title(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).FontSize(c.Theme().Rem(titleSize)).FontWeight(600).LineHeight(1.3)
}

// Mono shows code, a path or an identifier.
func Mono(c *ui.Context, s string) ui.Element {
	return ui.Text(c, s).Font(MonoFont).FontSize(c.Theme().Rem(helpSize))
}

// HelpSize returns the size of Help text, for elements styled alike.
func HelpSize(c *ui.Context) float32 { return c.Theme().Rem(helpSize) }

// CaptionSize returns the size of Caption text.
func CaptionSize(c *ui.Context) float32 { return c.Theme().Rem(captionSize) }
