# 检查项清单（结构分析，不是关键词猜测）

每一项都是 `.dev/check-frontend.cjs` 里一个真实的 `check(` 块（共 **169** 个，与运行时
报告的 169 项一致）。分类看的是**块体引用了哪些变量**，不是标签里的词：

| bucket | 判据 |
| --- | --- |
| `1-keep` | 只读 Go 源码或**生成的**绑定类型（`App.d.ts`/`models.ts`），或只读 CSS 且属窗口区域规则 |
| `2-rewrite` | 读 `mainJs` / `htmlIds` / `pluginHostJs`，即旧前端的实现细节 |
| `? 混合` | 同时读前端与 Go，要按它问的问题判 |
| `? 待人工` | 一个已知变量都没引用（多为读 `index.html` 原文或纯字符串断言） |

`2-rewrite` 的那些改成组件测试后可以删；`1-keep` 原样保留；`?` 两类逐条看。

| # | 起始行 | 检查项 | bucket |
| --- | --- | --- | --- |
| 1 | 126 | (第 126 行起的检查，标签在别处) | ? 待人工 |
| 2 | 145 | (第 145 行起的检查，标签在别处) | ? 待人工 |
| 3 | 160 | every App.* call is declared in App.d.ts | ? 待人工 |
| 4 | 169 | every window call the frontend applies as a layout is typed as one | ? 待人工 |
| 5 | 178 | the plugin surface types exist in models.ts | 1-keep |
| 6 | 187 | the Layout model carries the plugin surface geometry | 1-keep |
| 7 | 197 | the surface rectangle model carries the ellipse flag | 1-keep |
| 8 | 206 | the host transcript model is gone with the host conversation | 1-keep |
| 9 | 261 | (第 261 行起的检查，标签在别处) | ? 待人工 |
| 10 | 275 | the plugin and layout events are both emitted and subscribed | ? 待人工 |
| 11 | 317 | a section banner is missing | ? 待人工 |
| 12 | 328 | no box-shadow/filter/text-shadow/transform in the new CSS (they paint onto the desktop) | ? 待人工 |
| 13 | 336 | each new section banner appears in exactly one stylesheet | ? 待人工 |
| 14 | 345 | the deleted plugin tab has no banner left behind | ? 待人工 |
| 15 | 372 | (第 372 行起的检查，标签在别处) | ? 待人工 |
| 16 | 380 | every group has a heading | ? 待人工 |
| 17 | 397 | the settings sub-headings are exactly the pet\ | ? 待人工 |
| 18 | 403 | the pet settings carry no AI configuration | ? 待人工 |
| 19 | 412 | the flat layout left nothing behind | ? 待人工 |
| 20 | 429 | ),
  `a row uses its own layout: ${[...new Set(settingsRows)].join( | ? 待人工 |
| 21 | 434 | the field rows build on one shared label column | ? 待人工 |
| 22 | 445 | switch rows keep the checkbox beside its own text | ? 待人工 |
| 23 | 456 | the settings tab still scrolls inside the panel and sizes nothing itself | 2-rewrite |
| 24 | 488 | } / unexpected: ${extraSettings.join( | ? 待人工 |
| 25 | 495 | index.html has a conversation tab body | 2-rewrite |
| 26 | 500 | the tabs list includes the conversation tab | ? 待人工 |
| 27 | 508 | the tabs are todos/chat/records/settings | ? 待人工 |
| 28 | 517 | index.html has a conversation slot for the plugin | 2-rewrite |
| 29 | 522 | switchTab toggles the conversation tab with the others | 2-rewrite |
| 30 | 534 | the conversation tab button has its own id, separate from its body | 2-rewrite |
| 31 | 539 | the conversation tab is hidden when no plugin fills the slot | 2-rewrite |
| 32 | 549 | switchTab re-resolves the slot, which is what decides the tab exists | 2-rewrite |
| 33 | 567 | the conversation tab is watched while the panel is open | 2-rewrite |
| 34 | 580 | the watch is NOT bound to the tab being hidden | 2-rewrite |
| 35 | 588 | opening the panel re-reads the installed list at once | 2-rewrite |
| 36 | 603 | the plugin slot frame is rebuilt when the plugin VERSION changes | 2-rewrite |
| 37 | 608 | the floating surface frames are rebuilt when the version changes | 2-rewrite |
| 38 | 613 | the iframe carries the version it was loaded from | 2-rewrite |
| 39 | 620 | no leftover conversation card element | 2-rewrite |
| 40 | 631 | the "待办" jump is still available to the conversation plugin | ? 待人工 |
| 41 | 636 | Go reports which tab the frontend should show | ? 待人工 |
| 42 | 649 | the double click is detected from the pointer stream | 2-rewrite |
| 43 | 654 | a single click is deferred so a double click cannot flash the panel | 2-rewrite |
| 44 | 672 | the double-click branch was found | ? 待人工 |
| 45 | 677 | the second click cancels the pending single click before opening the surface | ? 待人工 |
| 46 | 683 | the double-click branch returns rather than scheduling a toggle | ? 待人工 |
| 47 | 688 | the deferred single click is what toggles the panel | 2-rewrite |
| 48 | 698 | a double click asks the conversation plugin for its surface | 2-rewrite |
| 49 | 708 | plugin-host.js positions every surface frame from the layout | 2-rewrite |
| 50 | 719 | an elliptical surface block is drawn as a true ellipse | 2-rewrite |
| 51 | 731 | the plugin frames are sandboxed to scripts only | 2-rewrite |
| 52 | 745 | the API key field is never populated from state | ? 待人工 |
| 53 | 757 | an untouched key field is omitted rather than sent empty | ? 待人工 |
| 54 | 771 | a finished focus does not auto-start the rest | 1-keep |
| 55 | 776 | answering the record is what starts the rest | 1-keep |
| 56 | 781 | the summary prompt cannot be dismissed | 1-keep |
| 57 | 788 | the plan prompt cannot be dismissed either | 1-keep |
| 58 | 802 | the pending flags are read before the window lock is taken | 1-keep |
| 59 | 812 | the frontend does not dismiss the summary prompt either | 2-rewrite |
| 60 | 822 | the frontend does not dismiss the plan prompt either | 2-rewrite |
| 61 | 834 | a reminder bubble is still dismissable | 2-rewrite |
| 62 | 849 | the backend refuses to open the surface over an unanswered question | ? 待人工 |
| 63 | 856 | the refusal names which question is in the way | 1-keep |
| 64 | 869 | the frontend turns the refused gesture into "go answer it" | 2-rewrite |
| 65 | 879 | the surface still takes the slot from a notification and the panel | 1-keep |
| 66 | 894 | the picker cannot be eaten by the popup either | 1-keep |
| 67 | 905 | the refusal names all three questions differently | 1-keep |
| 68 | 920 | Enter confirms the summary | 2-rewrite |
| 69 | 927 | Enter confirms the plan too | 2-rewrite |
| 70 | 939 | the plan prompt advertises Enter | 2-rewrite |
| 71 | 948 | the summary prompt gets its own placeholder back | 2-rewrite |
| 72 | 955 | the summary prompt advertises Enter | ? 待人工 |
| 73 | 960 | the Enter handler cannot leak into another bubble kind | 2-rewrite |
| 74 | 978 | the shortcuts are off by default | ? 待人工 |
| 75 | 985 | show is registered before hide | 1-keep |
| 76 | 992 | the shortcuts avoid the Ctrl+Shift combinations applications already use | 1-keep |
| 77 | 1023 | a failed registration stops the chain rather than continuing | 1-keep |
| 78 | 1030 | the shortcut is released when the feature is switched off | 1-keep |
| 79 | 1036 | the shortcuts are released on shutdown | 1-keep |
| 80 | 1043 | the tray offers both directions as a mouse fallback | 1-keep |
| 81 | 1053 | a real window call hides the pet rather than a CSS trick | ? 待人工 |
| 82 | 1062 | hiding is refused while a summary is waiting | 1-keep |
| 83 | 1069 | hiding is refused while the plan is waiting | 1-keep |
| 84 | 1078 | the hidden state is runtime-only, not persisted | 1-keep |
| 85 | 1088 | the settings switch is wired and its keys are shown | 2-rewrite |
| 86 | 1098 | the key labels come from Go rather than being written in the page | ? 混合 |
| 87 | 1107 | a refused registration is surfaced rather than silently swallowed | ? 混合 |
| 88 | 1115 | reminders are suppressed while the pet is hidden | ? 待人工 |
| 89 | 1132 | a parked record is re-offered after a restart | 1-keep |
| 90 | 1139 | a hidden pet is brought back for a question the timer is parked behind | 1-keep |
| 91 | 1150 | the pet is revealed WITHOUT being activated | 1-keep |
| 92 | 1162 | answering a question hides the pet again | 1-keep |
| 93 | 1172 | an explicit show is not undone by answering a question | 1-keep |
| 94 | 1181 | a failed reveal does not record a debt | 1-keep |
| 95 | 1200 | the pet settings carry no speech controls at all | 2-rewrite |
| 96 | 1213 | no retired speech binding is exported | 1-keep |
| 97 | 1221 | the pet has no speech fields in its own config | ? 待人工 |
| 98 | 1233 | the audio capability exists and is permission-gated | 1-keep |
| 99 | 1239 | the permission vocabulary is the expected seventeen, and by `net.ws` + `llm.borrow` + `process` + `net.credentials` + `plugin.share` + `timer.control` | 1-keep |
| 100 | 1272 | net.credentials is only ever declared together with net.fetch | ? 待人工 |
| 101 | 1295 | every permission a market plugin asks for exists in the host vocabulary | 1-keep |
| 102 | 1343 | the headless frame is mounted and hidden, and only for an eligible plugin | 2-rewrite |
| 103 | 1364 | audio is played from a FILE, never from memory | 1-keep |
| 104 | 1373 | every audio file name is checked before it becomes a path | 1-keep |
| 105 | 1394 | the two edge events are emitted as double-quoted literals | 1-keep |
| 106 | 1401 | the edge events carry facts and no wording | 1-keep |
| 107 | 1410 | a plugin can keep a headless frame, and only with a permission that fills one | 1-keep |
| 108 | 1422 | every eligible background plugin gets its own headless frame | 2-rewrite |
| 109 | 1433 | the headless frame is hidden, so it takes no space and no clicks | ? 待人工 |
| 110 | 1440 | the dispatcher gate is shared, not repeated per program | 1-keep |
| 111 | 1460 | the legacy voice library is migrated into the plugin | 1-keep |
| 112 | 1467 | the migration only READS its source | 1-keep |
| 113 | 1475 | the migration never points a line at a file that is missing | 1-keep |
| 114 | 1482 | migration slot names satisfy the audio capability | 1-keep |
| 115 | 1495 | no reply is read aloud | ? 待人工 |
| 116 | 1500 | nothing on the playback path synthesises | 1-keep |
| 117 | 1515 | the archived implementation is out of the build | ? 混合 |
| 118 | 1523 | the settings carry no synthesis fields | 2-rewrite |
| 119 | 1535 | the records list is grouped into collapsible days | 1-keep |
| 120 | 1545 | the newest day is the one that starts open | 2-rewrite |
| 121 | 1552 | a day the user collapsed stays collapsed across re-renders | 2-rewrite |
| 122 | 1561 | a newly appeared day opens by itself | 2-rewrite |
| 123 | 1568 | the per-session row no longer repeats the date | 2-rewrite |
| 124 | 1575 | the plan bubble has its own hint element | 2-rewrite |
| 125 | 1587 | the plan hint line is reset on every render | 2-rewrite |
| 126 | 1595 | the records row shows the plan above the actual result | 2-rewrite |
| 127 | 1606 | the records editor edits both lines in one write | 2-rewrite |
| 128 | 1616 | the two new bindings exist in the generated module and its types | 1-keep |
| 129 | 1639 | the records tab still scrolls inside the panel | 2-rewrite |
| 130 | 1652 | the scrollable surfaces share one scrollbar rule | ? 待人工 |
| 131 | 1662 | the records list and the chat log are on that rule | ? 待人工 |
| 132 | 1669 | the private scrollbar copies are gone | ? 待人工 |
| 133 | 1683 | the scrollbar is translucent and trackless | ? 待人工 |
| 134 | 1697 | the close button centres its glyph with flex, not line metrics | ? 待人工 |
| 135 | 1704 | the close button carries the measured optical offset | ? 待人工 |
| 136 | 1736 | both frontends actually load their stylesheet | 1-keep |
| 137 | 1742 | the voice plugin loads its own stylesheet and script | ? 待人工 |
| 138 | 1748 | the pet has no voice studio tab and no speech tab | 2-rewrite |
| 139 | 1756 | the pet has no in-panel way to open a voice studio | 2-rewrite |
| 140 | 1764 | the standalone voice studio is gone | ? 待人工 |
| 141 | 1773 | the voice plugin declares the capabilities it needs | ? 待人工 |
| 142 | 1780 | the voice plugin carries its built-in clips as declared assets | ? 待人工 |
| 143 | 1790 | the built-in clips are real audio, not empty placeholders | ? 待人工 |
| 144 | 1806 | rendering a take and adopting it are separate steps | ? 待人工 |
| 145 | 1815 | a missing take falls back to the packaged clip rather than to silence | ? 待人工 |
| 146 | 1823 | the plugin asks for bytes when it renders, not text | ? 待人工 |
| 147 | 1832 | the plugin never tries to start the engine itself | ? 待人工 |
| 148 | 1842 | the market can host a plugin interface | 1-keep |
| 149 | 1851 | the hosted interface is sandboxed to allow-scripts alone | 1-keep |
| 150 | 1862 | the market mounts an interface only for an installed and ENABLED plugin | ? 待人工 |
| 151 | 1917 | the plugin market is its own nested module reusing the pet package | ? 待人工 |
| 152 | 1923 | the plugin market builds to its own executable name | ? 待人工 |
| 153 | 1928 | the market frontend imports its own stylesheet | ? 待人工 |
| 154 | 1933 | the plugin market is a separate program keeping its own settings file | ? 待人工 |
| 155 | 1940 | the theme is a scheme on <html>, remembered in the market settings file | ? 待人工 |
| 156 | 1958 | the market top bar is a store header: mark, search, three window controls | ? 待人工 |
| 157 | 1996 | the market is two sibling views, with exactly one in the flow | ? 待人工 |
| 158 | 2011 | the market address is a field in the settings sheet, not a strip on the page | ? 待人工 |
| 159 | 2028 | the enable control is a switch whose wrapper is what gets hidden | ? 待人工 |
| 160 | 2036 | the market page lays the workspace beside a permission rail, and 设置 is a dialog | ? 待人工 |
| 161 | 2062 | the granted count lives in the permission panel header | ? 待人工 |
| 162 | 2075 | every icon in the market window is a symbol in its own sprite | ? 待人工 |
| 163 | 2103 | the hero states no status chip, and the controls are one bar at its end | ? 待人工 |
| 164 | 2123 | each bundled plugin follows the window’s colour scheme | 1-keep |
| 165 | 2145 | the market states a plugin\ | ? 待人工 |
| 166 | 2157 | the market never names the pet config, let alone writes it | ? 待人工 |
| 167 | 2173 | the market explains every permission the backend allows | 1-keep |
| 168 | 2241 | )} \| surplus: ` +
    `${onlyIn(harnessMethods, realMethods).join( | ? 待人工 |
| 169 | 2247 | )} \| surplus: ` +
    `${onlyIn(jsdomMethods, realMethods).join( | ? 待人工 |
