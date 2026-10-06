package portal

import "embed"

// FS embeds frontend assets: index.html, style.css, app.js, qr.js.
//
//go:embed index.html style.css app.js qr.js
var FS embed.FS
