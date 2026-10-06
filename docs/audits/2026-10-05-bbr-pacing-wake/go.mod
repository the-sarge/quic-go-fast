module bbrpacingwakeaids

// Keeps these finite #734 aids out of the root module's ./... packages.
// build.py copies them into exported modules; nothing builds them here.

go 1.27
