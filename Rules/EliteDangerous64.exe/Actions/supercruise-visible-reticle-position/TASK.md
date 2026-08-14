# Supercruise visible reticle position

Locate the selected-target reticle inside one 140×140 reference-pixel window
whose centre is supplied by the owning target-identity or tracking Action. The
hint is a bounded search origin, not a returned target position.

The Action derives three declared evidence planes from the same current RGB
region: the legacy strict RGB gate, a normalized orange-opponent plane, and an
HSV orange-hue plane. The latter two planes use their own
99.5-percentile-derived threshold with an absolute floor. This is
one primary adaptive classifier, not a provider or capture fallback: every
plane is evaluated on the same pixels, all thresholds and scores are returned,
and the selected adaptive plane is explicit.

The default `ADAPTIVE_ORANGE` policy keeps strict RGB diagnostic-only. The
explicit `OCCLUSION_AWARE` policy is reserved for a separately confirmed
`MOVE TO OBTAIN LINE OF SIGHT TO TARGET` workflow. It still prefers either
adaptive plane. Only when both adaptive planes reject the same current ROI may
a viable strict-RGB three-quarter annulus be selected. The returned selection
reason is
`OCCLUSION_AWARE_STRICT_RGB_SELECTED_AFTER_ADAPTIVE_REJECTION`; no provider,
capture, ROI, or prior-frame evidence changes silently. Exact current identity
and layout remain parent Gates before this local shape can authorize control.

Each plane evaluates a fixed grid of candidate centres. Its score rewards
pixels in the reviewed 34–58-pixel annulus, penalizes pixels in the adjacent
inner and outer clutter bands, and applies a small distance penalty from the
supplied hint. A distinct near-tied centre, insufficient annulus evidence,
non-positive radial contrast, or insufficient angular coverage rejects that
plane. A filled warm-colour field is also rejected because ED's reticle has an
intentional right-side label gap; zero angular transitions cannot establish a
reticle. If the two strongest viable planes point to distinct centres with
near-equal angular coverage, the Action returns `UNKNOWN` instead of silently
choosing one.

The selected annulus is divided into 72 angular bins without morphological
closing. The eighteen bins centred on the label-facing right side are treated
as the intentional one-quarter opening; the other fifty-four bins form the
required three-quarter structural arc. The Action publishes structural
coverage, label-gap clarity, radial contrast, centre uniqueness, and their
weighted `shapeConfidencePermille`. Shape confidence, rather than global image
contrast or OCR confidence, selects the evidence plane. One through four
occupied runs is `SOLID`; five or more is `DASHED`.
Solid topology additionally requires at least 40 occupied bins; dashed
topology requires at least 18. This rejects a few accidental warm-colour arcs
without demanding solid-ring coverage from a deliberately sparse dashed ring.
This preserves ED's actual topology: a solid ring still has its intentional
label gap, while a dashed ring contains repeated gaps. `occupiedAngularBins`
alone is retained as evidence but is no longer used as the solid/dashed rule.

This local CV deliberately ignores target identity. Initial acquisition must
come from `supercruise-target-position`; after that, a bounded controller may
track the already-identified reticle by feeding each current result back as the
next hint. A tracking miss authorizes no steering and must transition visibly
back to identity acquisition.

The ROI, candidate grid, evidence-plane order, and 64M Starlark step budget are
fixed. Exceeding the declared budget or receiving incomplete screen evidence
is a terminal infrastructure failure, not domain `UNKNOWN`.
