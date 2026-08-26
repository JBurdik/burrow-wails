package main

// appVersion is what CheckUpdate compares against the release manifest and
// what GetAppVersion reports to the UI. `just bump` rewrites it in lockstep
// with wails.json's info.productVersion (the source of truth) and
// package.json — don't edit it by hand.
const appVersion = "0.1.2"
