module bbrloopbackhandoffaids

// Keeps these finite #738 aids out of the root module's ./... packages.
// build.py copies them into exported trees; nothing builds them here.

go 1.27
