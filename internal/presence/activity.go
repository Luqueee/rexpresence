// Package presence describes the public activity exchanged between Rex and Discord.
package presence

// Activity is immutable after publication. A nil *Activity means clear the presence.
type Activity struct {
	Details string
	Group   string
	Project string
	Custom  bool  // Use only the explicit details, never automatic templates.
	Card    *Card // Immutable snapshot. Nil uses the default appearance.
	Started int64 // Unix timestamp in seconds.
}
