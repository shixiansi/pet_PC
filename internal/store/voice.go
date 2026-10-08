package store

// Voice studio defaults (docs/voice-studio.md).
//
// The studio renders the pet's fixed lines in a character the user picks, using a
// LOCAL GPT-SoVITS installation. Two defaults are therefore machine-specific
// paths, which the rest of this config deliberately avoids - but there is no
// sensible machine-independent value for "where is the voice engine", and a wrong
// guess costs one failed connection with a clear message rather than a broken
// feature. Both are editable in the settings panel.
const (
	// DefaultVoiceTTSBaseURL is where a local api_v2.py listens by default. 9880 is
	// the upstream default; the retired online bridge used 9881 so the two could
	// coexist, which is why they are different ports.
	DefaultVoiceTTSBaseURL = "http://127.0.0.1:9880"

	// DefaultVoiceTTSDir is this machine's GPT-SoVITS checkout. The character
	// models live under <dir>/models and the interpreter that can import the engine
	// under <dir>/.venv.
	DefaultVoiceTTSDir = `D:\GPT-SoVITS`
)
