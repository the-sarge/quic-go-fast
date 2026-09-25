package model

import (
	"fmt"
	"math"
	"time"
)

// Scenario fixes the accepted IP-byte domain and first-phase L3 queue basis.
// S1 deliberately uses native routing with this emulator stopped.
func Scenario(id string, forward bool, phase int, seed uint64, mtu int) (*Queue, error) {
	if mtu != 1460 || phase < 0 || phase > 12 || phase%3 != 0 {
		return nil, fmt.Errorf("expected MTU 1460 and event phase 0,3,6,9,12")
	}
	rates := map[string][3]float64{
		"S2": {20, 100, 150}, "S3": {1000, 1000, 200}, "S4": {20, 100, 250}, "S5": {100, 100, 100}, "S6": {100, 100, 100}, "S7": {100, 100, 100}, "S8": {100, 100, 100},
		"L0": {20, 100, 40}, "L1": {20, 100, 40}, "L2": {20, 100, 40}, "L3": {5, 30, 50}, "L4": {4, 4, 1000}, "L5": {.256, .256, 1000}, "L6": {100, 100, 100}, "L7": {100, 100, 100}, "L8": {100, 100, 100},
	}
	values, ok := rates[id]
	if !ok {
		return nil, fmt.Errorf("unknown modeled scenario %q", id)
	}
	index := 0
	if !forward {
		index = 1
	}
	rate := int64(values[index] * 1e6)
	multiplier := 1
	if id == "S7" {
		multiplier = 4
	}
	capacity := max(2, int(math.Ceil(float64(rate)*values[2]/1000/8/float64(mtu)*float64(multiplier)))) * mtu
	d := Direction{Rate: rate, Capacity: capacity, Delay: time.Duration(values[2] * float64(time.Millisecond) / 2), Mark: id == "S8", Seed: seed}
	if id == "S6" && forward {
		d.Loss = .001
	}
	s := Schedule{Phase: time.Duration(phase) * time.Second, Competitors: id == "L6" || id == "L7" || id == "L8"}
	if forward && (id == "L1" || id == "L2") {
		s.Kind = id
	}
	if id == "L3" {
		s.Kind = id
		if forward {
			s.Changes = []RateChange{{90 * time.Second, 20000000, 15 * time.Millisecond}, {180 * time.Second, 1000000, 40 * time.Millisecond}, {240 * time.Second, 20000000, 15 * time.Millisecond}}
		} else {
			s.Changes = []RateChange{{90 * time.Second, 150000000, 15 * time.Millisecond}, {180 * time.Second, 10000000, 40 * time.Millisecond}, {240 * time.Second, 150000000, 15 * time.Millisecond}}
		}
	}
	return New(d, s), nil
}
