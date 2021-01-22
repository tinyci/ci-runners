from "docker:dind"

after { flatten }

run "apk add git"
copy "release.tar.gz", "/tmp/release.tar.gz"
run "cd /tmp && tar vxzf release.tar.gz && chown -R root:root /tmp/build && mv /tmp/build/* /usr/local/bin && rm -r /tmp/release.tar.gz /tmp/build"

copy "entrypoint.sh", "/entrypoint.sh"
run "chmod 755 /entrypoint.sh"

entrypoint "/entrypoint.sh"
