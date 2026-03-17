# beaut: Convenient handling for validated types

[![Go Reference](https://pkg.go.dev/badge/github.com/sourcegraph/beaut.svg)](https://pkg.go.dev/github.com/sourcegraph/beaut)
[![Sourcegraph](https://img.shields.io/badge/view%20on-sourcegraph-A112FE?logo=data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAADIAAAAyCAYAAAAeP4ixAAAEZklEQVRoQ+2aXWgUZxSG3292sxtNN43BhBakFPyhxSujRSxiU1pr7SaGXqgUxOIEW0IFkeYighYUxAuLUlq0lrq2iCDpjWtmFVtoG6QVNOCFVShVLyxIk0DVjZLMxt3xTGTccd2ZOd/8JBHci0CY9zvnPPN+/7sCIXwKavOwAcy2QgngQiIztDSE0OwQlDPYR1ebiaH6J5kZChyfW12gRG4QVgGTBfMchMbFP9Sn5nlZL2D0JjLD6710lc+z0NfqSGTXQRQ4bX07Mq423yoBL3OSyHSvUxirMuaEvgbJWrdcvkHMoJwxYuq4INUhyuWvQa1jvdMGxAvCxJlyEC9XOBCWL04wwRzpbDoDQ7wfZJzIQLi5Eggk6DiRhZgWIAbE3NrM4A3LPT8Q7UgqAqLqTmLSHLGPkyzG/qXEczhd0q6RH+zaSBfaUoc4iQx19pIClIscrTkNZzG6gd7qMY6eC2Hqyo705ZfTf+eqJmhMzcSbYtQpOXc92ZsZjLVAL4YNUQbJ5Ttg4CQrQdGYj44Xr9m1XJCzmZusFDJOWNpHjmh5x624a2ZFtOKDVL+uNo2TuXE3bZQQZUf8gtgqP31uI94Z/rMqix+IGiRfWw3xN9dCgVx+L3WrHm4Dju6PXz/EkjuXJ6R+IGgyOE1TbZqTq9y1eo0EZo7oMo1ktPu3xjHvuiLT5AFNszUyDULtWpzE2/fEsey8O5TbWuGWwxrs5rS7nFNMWJrNh2No74s9Ec4vRNmRRzPXMP19fBMSVsGcOJ98G8N3Wl2gXcbTjbX7vUBxLaeASDQCm5Cu/0E2tvtb0Ea+BowtskFD0wvlc6Rf2M+Jx7dTu7ubFr2dnKDRaMQe2v/tcIrNB7FH0O50AcrBaApmRDVwFO31ql3pD8QW4dP0feNwl/Q+kFEtRyIGyaWXnpy1OO0qNJWHo1y6iCmAGkBb/Ru+HenDWIF2mo4r8G+tRRzoniSn2uqFLxANhe9LKHVyTbz6egk9+x5w5fK6ulSNNMhZ/Feno+GebLZV6isTTa6k5qNl5RnZ5u56Ib6SBvFzaWBBVFZzvnERWlt/Cg4l27XChLCqFyLekjhy6xJyoytgjPf7opIB8QPx7sYFiMXHPGt76m741MhCKMZfng0nBOIjmoJPsLqWHwgFpe6V6qtfcopxveR2Oy+J0ntIN/zCWkf8QNAJ7y6d8Bq4lxLc2/qJl5K7t432XwcqX5CrI34gzATWuYILQtdQPyePDK3iuOekCR3Efjhig1B1Uq5UoXEEoZX7d1q535J5S9VOeFyYyEBku5XTMXXKQTToX5Rg7OI44nbW5oKYeYK4EniMeF0YFNSmb+grhc84LyRCEP1/OurOcipCQbKxDeK2V5FcVyIDMQvsgz5gwFhcWWwKyRlvQ3gv29RwWoDYAbIofNyBxI9eDlQ+n3YgsgCWnr4MStGXQXmv9pF2La/k3OccV54JEBM4yp9EsXa/3LfO0dGPcYq0Y7DfZB8nJzZw2rppHgKgVHs8L5wvRwAAAABJRU5ErkJggg==)](https://sourcegraph.com/github.com/sourcegraph/beaut)

In certain situations, you've already done some validation 
on a type. For example, you may have checked that a path
is an absolute path, or that a string is valid UTF-8.
However, since there is no standard library support for
representing such types, maybe you continue passing that
value around as a `string` or a `[]byte`. This has a few downsides:

- Reduced dev velocity: It is not clear to the reader
  which operations are safe to perform, and editing the code
  becomes harder as it is unclear which invariants must be upheld.
- Poor debugging experience: Failures may appear much further downstream
  in case validation was not done on all code paths.
- Performance cost: Out of defensiveness, different parts of the
  code may perform the same validation redundantly.

Following the [Parse, Don't Validate](https://lexi-lambda.github.io/blog/2019/11/05/parse-don-t-validate/)
mantra, it is useful to have a dedicated type to represent
the validation, so that it is clearer to the reader which
operations are safe to perform and which operations require
extra care.

This library exposes a few types such as `UTF8String`, `UTF8Bytes`,
`AbsolutePath` and `RelativePath` to represent common validations.
