from "ubuntu:20.04"

after { flatten }

skip { copy "release.tar.gz", "/" }
run "tar -vxz -C /usr/bin --strip-components=1 -f release.tar.gz"
