// Package wind räknar ut hur kall en temperatur känns när vinden räknas in
// ("Känns som"). Temperaturen kommer från mätpunkten och vinden från den
// närmaste vindstationen hos SMHI enligt en fast mappning (se Mapping).
package wind

import "math"

// Gränserna för när SMHI:s formel för vindkyla gäller.
const (
	chillMinTemp  = -40.0 // °C
	chillMaxTemp  = 10.0  // °C
	chillMinSpeed = 1.3   // m/s
	chillMaxSpeed = 35.0  // m/s
)

// FeelsLike returnerar den effektiva temperaturen i °C för lufttemperaturen
// temp (°C) och vindhastigheten speed (m/s, på 10 m höjd).
//
// Formeln är den SMHI använder sedan vintern 2003/2004 (Osczevski och
// Bluestein, SMHI:s faktablad nr 17). Utanför formelns giltighetsområde
// påverkar vinden inte resultatet, och temp returneras oförändrad.
func FeelsLike(temp, speed float64) float64 {
	if !chillApplies(temp, speed) {
		return temp
	}
	v := math.Pow(speed, 0.16)
	return 13.126667 + 0.6215*temp - 13.924748*v + 0.4875195*temp*v
}

func chillApplies(temp, speed float64) bool {
	return temp >= chillMinTemp && temp <= chillMaxTemp &&
		speed >= chillMinSpeed && speed <= chillMaxSpeed
}
