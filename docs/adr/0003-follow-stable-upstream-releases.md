---
status: accepted
---

# Base fork releases on validated stable upstream releases

Fork releases are based on stable upstream releases, with selected unreleased fixes brought in when needed. Upstream security fixes receive prompt review, and each upstream update is validated before adoption. Our patches remain independently reviewable so each change can be assessed, carried forward, or retired when upstream provides an equivalent remedy.

Upstream acceptance of a fork improvement is welcome but is not a prerequisite for releasing it here. Tracking every upstream development commit automatically is outside this policy. Released fork versions and their upstream base must be identifiable so consumers can pin the version they validated and performance comparisons can distinguish upstream changes from fork changes. This decision does not create an automated monitoring service or promise a fixed response-time SLA.
