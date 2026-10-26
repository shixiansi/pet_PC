# 检查项清单（编号来自脚本自身的输出）

这份清单由 `.dev/check-frontend.cjs` 自己的 `PASS` 输出生成，所以条目与运行时报告的
数字一一对应 —— 不再是正则估出来的近似值。

`bucket` 是**第一遍启发式**分类，按标签里的关键词猜的。它只用来分派工作，
不能当作结论：动手前每条都要看一眼它的检查体，确认它读的是 `mainJs` / `htmlIds`
（= 2-rewrite）还是 Go 源码（= 1-keep）。`?` 表示关键词没命中，需要人判。

| # | 检查项 | bucket |
| --- | --- | --- |
| 1 | every element id referenced from JS exists in index.html (85 ids) | 2-rewrite |
| 2 | every App.* call is an exported binding (43 calls, 59 exports) | 1-keep |
| 3 | every App.* call is declared in App.d.ts | ? |
| 4 | every window call the frontend applies as a layout is typed as one | 2-rewrite |
| 5 | the plugin surface types exist in models.ts | 1-keep |
| 6 | the Layout model carries the plugin surface geometry | 1-keep |
| 7 | the surface rectangle model carries the ellipse flag | 1-keep |
| 8 | the host transcript model is gone with the host conversation | ? |
| 9 | every subscribed event is emitted by Go (5 subscriptions) | ? |
| 10 | the plugin and layout events are both emitted and subscribed | 2-rewrite |
| 11 | all 4 checked CSS sections are present | 2-rewrite |
| 12 | no box-shadow/filter/text-shadow/transform in the new CSS (they paint onto the desktop) | 1-keep |
| 13 | each new section banner appears in exactly one stylesheet | 2-rewrite |
| 14 | the deleted plugin tab has no banner left behind | 2-rewrite |
| 15 | the settings page is four named groups (4 found) | ? |
| 16 | every group has a heading | ? |
| 17 | the settings sub-headings are exactly the pet's own | ? |
| 18 | the pet settings carry no AI configuration | ? |
| 19 | the flat layout left nothing behind | 2-rewrite |
| 20 | every settings row uses a shared shape (30 field rows) | ? |
| 21 | the field rows build on one shared label column | ? |
| 22 | switch rows keep the checkbox beside its own text | ? |
| 23 | the settings tab still scrolls inside the panel and sizes nothing itself | 2-rewrite |
| 24 | the settings page still carries every control (34 ids) | 2-rewrite |
| 25 | index.html has a conversation tab body | 2-rewrite |
| 26 | the tabs list includes the conversation tab | 2-rewrite |
| 27 | the tabs are todos/chat/records/settings | 2-rewrite |
| 28 | index.html has a conversation slot for the plugin | 1-keep |
| 29 | switchTab toggles the conversation tab with the others | 2-rewrite |
| 30 | the conversation tab button has its own id, separate from its body | 2-rewrite |
| 31 | the conversation tab is hidden when no plugin fills the slot | 1-keep |
| 32 | switchTab re-resolves the slot, which is what decides the tab exists | 1-keep |
| 33 | the conversation tab is watched while the panel is open | 2-rewrite |
| 34 | the watch is NOT bound to the tab being hidden | 2-rewrite |
| 35 | opening the panel re-reads the installed list at once | ? |
| 36 | the plugin slot frame is rebuilt when the plugin VERSION changes | 1-keep |
| 37 | the floating surface frames are rebuilt when the version changes | 1-keep |
| 38 | the iframe carries the version it was loaded from | 1-keep |
| 39 | no leftover conversation card element | 2-rewrite |
| 40 | the "待办" jump is still available to the conversation plugin | ? |
| 41 | Go reports which tab the frontend should show | 2-rewrite |
| 42 | the double click is detected from the pointer stream | ? |
| 43 | a single click is deferred so a double click cannot flash the panel | ? |
| 44 | the double-click branch was found | ? |
| 45 | the second click cancels the pending single click before opening the surface | 1-keep |
| 46 | the double-click branch returns rather than scheduling a toggle | ? |
| 47 | the deferred single click is what toggles the panel | ? |
| 48 | a double click asks the conversation plugin for its surface | 1-keep |
| 49 | plugin-host.js positions every surface frame from the layout | 1-keep |
| 50 | an elliptical surface block is drawn as a true ellipse | 1-keep |
| 51 | the plugin frames are sandboxed to scripts only | 1-keep |
| 52 | the API key field is never populated from state | ? |
| 53 | an untouched key field is omitted rather than sent empty | ? |
| 54 | a finished focus does not auto-start the rest | ? |
| 55 | answering the record is what starts the rest | ? |
| 56 | the summary prompt cannot be dismissed | ? |
| 57 | the plan prompt cannot be dismissed either | ? |
| 58 | the pending flags are read before the window lock is taken | 1-keep |
| 59 | the frontend does not dismiss the summary prompt either | ? |
| 60 | the frontend does not dismiss the plan prompt either | ? |
| 61 | a reminder bubble is still dismissable | ? |
| 62 | the backend refuses to open the surface over an unanswered question | 1-keep |
| 63 | the refusal names which question is in the way | ? |
| 64 | the frontend turns the refused gesture into "go answer it" | ? |
| 65 | the surface still takes the slot from a notification and the panel | 1-keep |
| 66 | the picker cannot be eaten by the popup either | ? |
| 67 | the refusal names all three questions differently | ? |
| 68 | Enter confirms the summary | ? |
| 69 | Enter confirms the plan too | ? |
| 70 | the plan prompt advertises Enter | ? |
| 71 | the summary prompt gets its own placeholder back | ? |
| 72 | the summary prompt advertises Enter | ? |
| 73 | the Enter handler cannot leak into another bubble kind | ? |
| 74 | the shortcuts are off by default | ? |
| 75 | show is registered before hide | ? |
| 76 | the shortcuts avoid the Ctrl+Shift combinations applications already use | 2-rewrite |
| 77 | a failed registration stops the chain rather than continuing | ? |
| 78 | the shortcut is released when the feature is switched off | ? |
| 79 | the shortcuts are released on shutdown | ? |
| 80 | the tray offers both directions as a mouse fallback | ? |
| 81 | a real window call hides the pet rather than a CSS trick | 2-rewrite |
| 82 | hiding is refused while a summary is waiting | ? |
| 83 | hiding is refused while the plan is waiting | ? |
| 84 | the hidden state is runtime-only, not persisted | 2-rewrite |
| 85 | the settings switch is wired and its keys are shown | ? |
| 86 | the key labels come from Go rather than being written in the page | ? |
| 87 | a refused registration is surfaced rather than silently swallowed | 1-keep |
| 88 | reminders are suppressed while the pet is hidden | 2-rewrite |
| 89 | a parked record is re-offered after a restart | ? |
| 90 | a hidden pet is brought back for a question the timer is parked behind | 2-rewrite |
| 91 | the pet is revealed WITHOUT being activated | ? |
| 92 | answering a question hides the pet again | ? |
| 93 | an explicit show is not undone by answering a question | ? |
| 94 | a failed reveal does not record a debt | ? |
| 95 | the pet settings carry no speech controls at all | ? |
| 96 | no retired speech binding is exported | 1-keep |
| 97 | the pet has no speech fields in its own config | ? |
| 98 | the audio capability exists and is permission-gated | 1-keep |
| 99 | the permission vocabulary is the expected seventeen, and by `net.ws` + `llm.borrow` + `process` + `net.credentials` + `plugin.share` + `timer.control` | 1-keep |
| 100 | net.credentials is only ever declared together with net.fetch | 1-keep |
| 101 | every permission a market plugin asks for exists in the host vocabulary | 1-keep |
| 102 | the headless frame is mounted and hidden, and only for an eligible plugin | 1-keep |
| 103 | audio is played from a FILE, never from memory | 1-keep |
| 104 | every audio file name is checked before it becomes a path | 1-keep |
| 105 | the two edge events are emitted as double-quoted literals | ? |
| 106 | the edge events carry facts and no wording | ? |
| 107 | a plugin can keep a headless frame, and only with a permission that fills one | 1-keep |
| 108 | every eligible background plugin gets its own headless frame | ? |
| 109 | the headless frame is hidden, so it takes no space and no clicks | 1-keep |
| 110 | the dispatcher gate is shared, not repeated per program | ? |
| 111 | the legacy voice library is migrated into the plugin | ? |
| 112 | the migration only READS its source | ? |
| 113 | the migration never points a line at a file that is missing | ? |
| 114 | migration slot names satisfy the audio capability | 1-keep |
| 115 | no reply is read aloud | ? |
| 116 | nothing on the playback path synthesises | ? |
| 117 | the archived implementation is out of the build | ? |
| 118 | the settings carry no synthesis fields | ? |
| 119 | the records list is grouped into collapsible days | ? |
| 120 | the newest day is the one that starts open | ? |
| 121 | a day the user collapsed stays collapsed across re-renders | ? |
| 122 | a newly appeared day opens by itself | ? |
| 123 | the per-session row no longer repeats the date | ? |
| 124 | the plan bubble has its own hint element | 2-rewrite |
| 125 | the plan hint line is reset on every render | ? |
| 126 | the records row shows the plan above the actual result | ? |
| 127 | the records editor edits both lines in one write | ? |
| 128 | the two new bindings exist in the generated module and its types | 1-keep |
| 129 | the records tab still scrolls inside the panel | 2-rewrite |
| 130 | the scrollable surfaces share one scrollbar rule | 1-keep |
| 131 | the records list and the chat log are on that rule | ? |
| 132 | the private scrollbar copies are gone | 2-rewrite |
| 133 | the scrollbar is translucent and trackless | 2-rewrite |
| 134 | the close button centres its glyph with flex, not line metrics | 2-rewrite |
| 135 | the close button carries the measured optical offset | 2-rewrite |
| 136 | both frontends actually load their stylesheet | 2-rewrite |
| 137 | the voice plugin loads its own stylesheet and script | 2-rewrite |
| 138 | the pet has no voice studio tab and no speech tab | 2-rewrite |
| 139 | the pet has no in-panel way to open a voice studio | ? |
| 140 | the standalone voice studio is gone | ? |
| 141 | the voice plugin declares the capabilities it needs | ? |
| 142 | the voice plugin carries its built-in clips as declared assets | 1-keep |
| 143 | the built-in clips are real audio, not empty placeholders | 1-keep |
| 144 | rendering a take and adopting it are separate steps | 1-keep |
| 145 | a missing take falls back to the packaged clip rather than to silence | 1-keep |
| 146 | the plugin asks for bytes when it renders, not text | ? |
| 147 | the plugin never tries to start the engine itself | ? |
| 148 | the market can host a plugin interface | ? |
| 149 | the hosted interface is sandboxed to allow-scripts alone | 1-keep |
| 150 | the market mounts an interface only for an installed and ENABLED plugin | 2-rewrite |
| 151 | the plugin market is its own nested module reusing the pet package | ? |
| 152 | the plugin market builds to its own executable name | 2-rewrite |
| 153 | the market frontend imports its own stylesheet | 2-rewrite |
| 154 | the plugin market is a separate program keeping its own settings file | ? |
| 155 | the theme is a scheme on <html>, remembered in the market settings file | ? |
| 156 | the market top bar is a store header: mark, search, three window controls | ? |
| 157 | the market is two sibling views, with exactly one in the flow | ? |
| 158 | the market address is a field in the settings sheet, not a strip on the page | ? |
| 159 | the enable control is a switch whose wrapper is what gets hidden | 2-rewrite |
| 160 | the market page lays the workspace beside a permission rail, and 设置 is a dialog | 1-keep |
| 161 | the granted count lives in the permission panel header | 1-keep |
| 162 | every icon in the market window is a symbol in its own sprite | ? |
| 163 | the hero states no status chip, and the controls are one bar at its end | ? |
| 164 | each bundled plugin follows the window’s colour scheme | ? |
| 165 | the market states a plugin's state as a dot on its card icon | ? |
| 166 | the market never names the pet config, let alone writes it | ? |
| 167 | the market explains every permission the backend allows | 1-keep |
| 168 | the headless harness fakes exactly the bindings Go exports (59) | 1-keep |
| 169 | the jsdom harness fakes exactly the bindings Go exports (59) | 1-keep |
