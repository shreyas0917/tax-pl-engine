// Package all registers every broker parser. Import it for side effects:
//
//	import _ "github.com/shreyas0917/taxpl/internal/parser/all"
//
// Adding a broker means adding one import line here and nothing else in the core.
package all

import (
	_ "github.com/shreyas0917/taxpl/internal/parser/angelone" // Angel One trade history (Trades and charges)
	_ "github.com/shreyas0917/taxpl/internal/parser/groww"    // Groww stocks order history
	_ "github.com/shreyas0917/taxpl/internal/parser/upstox"   // Upstox trade report
	_ "github.com/shreyas0917/taxpl/internal/parser/zerodha"  // Zerodha Console tradebook
)
