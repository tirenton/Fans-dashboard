// PROBE_CONTROLS=0 hides the probe's controls (Admin page, Pull now). The VPS shows them by default: they become
// orders the home PC collects (api/remote.go), when the PC allows it with VPS_REMOTE_CONTROL=1.
export const probeControls = () => process.env.PROBE_CONTROLS !== "0";
