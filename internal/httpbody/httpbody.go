// Package httpbody hanterar svarskroppar från HTTP-anrop som programmet gör.
package httpbody

import "io"

// Drain läser och kastar högst limit byte av body. Det gör att anslutningen
// kan återanvändas när svaret inte ska läsas. Fel ignoreras med avsikt: då
// stängs anslutningen i stället, och anroparen har redan fått det den behöver.
func Drain(body io.Reader, limit int64) {
	_, _ = io.Copy(io.Discard, io.LimitReader(body, limit))
}
