package main

import (
	"database/sql"
	"fmt"
	"os"
	"sort"
	"time"

	"github.com/art-injener/satellite-scout/internal/tracker"
	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", "file:data/scout.db?mode=ro")
	if err != nil {
		panic(err)
	}
	defer db.Close()
	obs := tracker.NewObserver(47.2357, 39.7015, 0.07)
	now := time.Now().UTC()
	mode := "uhf"
	if len(os.Args) > 1 {
		mode = os.Args[1]
	}
	if mode == "passes" {
		norads := []int{25544, 25338, 28654, 33591, 59051, 63217, 63219}
		for _, n := range norads {
			var name, l1, l2 string
			if err := db.QueryRow(`select name,line1,line2 from orbits where norad=?`, n).Scan(&name, &l1, &l2); err != nil {
				fmt.Printf("%d no-tle\n", n)
				continue
			}
			tle := &tracker.TLE{Name: name, Line1: l1, Line2: l2}
			prop, err := tracker.NewPropagator(tle)
			if err != nil {
				continue
			}
			passes, err := tracker.PredictPasses(prop, obs, now, now.Add(8*time.Hour), 5)
			if err != nil || len(passes) == 0 {
				fmt.Printf("%d %-22s no pass\n", n, name)
				continue
			}
			p := passes[0]
			fmt.Printf("%d %-22s aos=%s maxel=%.1f los=%s\n", n, name,
				p.AOSTime().Local().Format("15:04:05"), p.TCAEl, p.LOSTime().Local().Format("15:04:05"))
		}
		return
	}
	rows, err := db.Query(`select o.norad,o.name,o.line1,o.line2 from orbits o`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	type hit struct {
		n      int
		name   string
		el, az float64
		uhf    bool
	}
	var vis []hit
	for rows.Next() {
		var n int
		var name, l1, l2 string
		if err := rows.Scan(&n, &name, &l1, &l2); err != nil {
			continue
		}
		tle := &tracker.TLE{Name: name, Line1: l1, Line2: l2}
		prop, err := tracker.NewPropagator(tle)
		if err != nil {
			continue
		}
		eci, err := prop.Propagate(now)
		if err != nil {
			continue
		}
		aer := obs.GetAER(eci)
		if aer.ElDeg() < 5 {
			continue
		}
		var uhf int
		_ = db.QueryRow(`select count(*) from transmitters where norad=? and downlink_hz between 144000000 and 450000000`, n).Scan(&uhf)
		vis = append(vis, hit{n, name, aer.ElDeg(), aer.AzDeg(), uhf > 0})
	}
	sort.Slice(vis, func(i, j int) bool { return vis[i].el > vis[j].el })
	for _, h := range vis {
		if mode == "uhf" && !h.uhf {
			continue
		}
		mark := ""
		if h.uhf {
			mark = " UHF/VHF"
		}
		fmt.Printf("%d %-24s el=%5.1f az=%5.1f%s\n", h.n, h.name, h.el, h.az, mark)
	}
}
