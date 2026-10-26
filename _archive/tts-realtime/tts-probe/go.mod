// This module exists only to hold throwaway diagnostic probes for
// docs/genshin-tts.md. It is deliberately separate from the PetPC module so no
// probe dependency can ever leak into the product build.
module ttsprobe

go 1.22.0

require golang.org/x/sys v0.30.0
