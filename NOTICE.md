karvi
Copyright (c) 2026 karvi contributors

karvi is released under the MIT License (see LICENSE.md).

The source tree vendors the following third-party Go modules under vendor/,
each with its own licence file at the path given:

- github.com/scrapli/scrapligo (MIT License), vendor/github.com/scrapli/scrapligo/LICENSE
- github.com/creack/pty (MIT License, copyright Keith Rarick), vendor/github.com/creack/pty/LICENSE
- github.com/sirikothe/gotextfsm (Apache License 2.0), vendor/github.com/sirikothe/gotextfsm/LICENSE
- golang.org/x/crypto (BSD 3-Clause License, copyright The Go Authors), vendor/golang.org/x/crypto/LICENSE
- golang.org/x/sys (BSD 3-Clause License, copyright The Go Authors), vendor/golang.org/x/sys/LICENSE
- golang.org/x/term (BSD 3-Clause License, copyright The Go Authors), vendor/golang.org/x/term/LICENSE
- github.com/yuin/goldmark (MIT License, copyright Yusuke Inuzuka), vendor/github.com/yuin/goldmark/LICENSE,
  used by tools/md-to-html alone and in no executable

karvi invokes the system OpenSSH client at runtime when the system transport
is selected. OpenSSH is separately licensed by its distributors and is not
embedded in these executables.
