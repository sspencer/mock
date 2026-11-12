package mockhttp

import (
	"encoding/binary"
	"hash/fnv"
	"math"
	"math/rand/v2"
	"strconv"
	"sync/atomic"

	"github.com/sspencer/mock/restclient"
)

// rollChance reports whether a route with @match.chance accepts this
// otherwise matching request. Routes without one always do. Under @seed the
// nth draw of a route depends only on the seed, the route and n, so a fresh
// server sent the same requests in the same order picks the same routes.
func rollChance(method *restclient.Method, rolls *atomic.Uint64) bool {
	if method.Chance <= 0 {
		return true
	}
	n := rolls.Add(1)
	if seed, ok := method.Variable("seed"); ok {
		h := fnv.New64a()
		h.Write([]byte(seed))
		h.Write([]byte{0})
		h.Write([]byte(routeKey(*method)))
		h.Write(binary.BigEndian.AppendUint64(nil, n))
		return rand.New(rand.NewPCG(h.Sum64(), 0x6d6f636b)).Float64() < method.Chance
	}
	return rand.Float64() < method.Chance
}

// describeChance formats a route's @match.chance, such as "20%", or "" when
// it has none.
func describeChance(method restclient.Method) string {
	if method.Chance <= 0 {
		return ""
	}
	// Rounding hides float noise such as 7.000000000000001.
	return strconv.FormatFloat(math.Round(method.Chance*1e6)/1e4, 'f', -1, 64) + "%"
}
