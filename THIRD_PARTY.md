# Third-party components

FM-STEP itself is GPL-3.0-or-later (see LICENSE). The components below keep their own licences; all are compatible with the GPL, and their notices are kept here and travel with every card build.

| Component | Used for | Licence | Where |
|---|---|---|---|
| **Plaits** by Emilie Gillet / Mutable Instruments | the 23 sound models | MIT | fetched by `dsp/build.sh` from github.com/pichenettes/eurorack (pinned commit), not stored here |
| **Clouds** reverb by Emilie Gillet / Mutable Instruments | reverb (FM-STEP keeps a float-storage copy in `dsp/reverb_f32.h`) | MIT | eurorack (as above); derived file in `dsp/reverb_f32.h` |
| **stmlib** by Emilie Gillet | DSP utilities used by Plaits and Clouds | MIT | fetched from github.com/pichenettes/stmlib (pinned commit) |
| **Inter** by Rasmus Andersson | interface typeface, pre-rasterised into `internal/gfx/fonts_gen.go` by `tools/gen_fonts.py` | SIL Open Font License 1.1 | rsms.me/inter |
| **am-mod** by Aleksey Malykh | the `autorun.json` / `autorun.check` format and the 1.3.0.12 stock hashes; a reference for the step sequencer | MIT | github.com/AlekseyMalykh/am-mod (no code copied) |
| **virtual-mpc-sample** | control-surface and platform facts | MIT | github.com/orrpunk-art/virtual-mpc-sample (no code copied) |
| **mpc-sample-lab** | protocol facts only (button LEDs, control surface, audio card and input switches) | no licence (all rights reserved) | github.com/gospodindilly-ops/mpc-sample-lab (facts only, no code or text copied) |
| **MPC-Sample-Toolkit** by tarikcampos | reference for the stock `.xpj` project format | MIT | github.com/tarikcampos/MPC-Sample-Toolkit (no code copied so far) |
| **az0x-open-source** by inMusic | hardware facts from inMusic's GPL source release (device tree `rk3566-az07-ac50.dts`: audio routing, inputs) | per package (kernel GPL-2.0; the device tree GPL-2.0+ OR MIT) | github.com/inmusic-software/az0x-open-source (facts only, no code copied) |

MIT notice for Mutable Instruments code (Plaits, Clouds, stmlib):

> Copyright 2014-2021 Emilie Gillet. Permission is hereby granted, free of charge, to any person obtaining a copy of this software and associated documentation files (the "Software"), to deal in the Software without restriction, including without limitation the rights to use, copy, modify, merge, publish, distribute, sublicense, and/or sell copies of the Software, and to permit persons to whom the Software is furnished to do so, subject to the following conditions: The above copyright notice and this permission notice shall be included in all copies or substantial portions of the Software. THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND.

No Akai / inMusic code, firmware or assets are included or redistributed. The stock firmware is never modified.
