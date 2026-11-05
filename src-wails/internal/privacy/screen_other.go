//go:build !(darwin && cgo)

package privacy

func ScreenRecording() string        { return "unknown" }
func RequestScreenRecording() string { return "unknown" }
func Accessibility() string          { return "unknown" }
func RequestAccessibility() string   { return "unknown" }
