module bbrlinuxredemoaids

// Keeps these finite #715 aids out of the root module's ./... packages.
// build.py copies them into exported campaign modules; nothing builds them here.

go 1.27
