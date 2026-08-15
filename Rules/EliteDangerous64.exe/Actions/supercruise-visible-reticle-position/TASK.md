# Supercruise visible reticle position

Locate the selected-target reticle inside one 140×140 reference-pixel window
whose centre is supplied by the owning target-identity or tracking Action. The
hint is a bounded search origin, not a returned target position.

The Action derives three declared evidence planes from the same current RGB
region: strict RGB, normalized orange-opponent, and alpha-invariant orange hue
plus saturation. The alpha-invariant score deliberately excludes HSV Value, so
uniform HUD dimming does not weaken otherwise identical orange evidence. It
uses one fixed reviewed score Gate. The opponent plane uses one deterministic
Otsu threshold over non-zero current-frame scores; strict RGB retains its fixed
Gate. All-zero, constant, or fewer than 24 non-zero scores explicitly reject a
required adaptive plane. The classifier never retries a threshold, changes
provider, captures another frame, repairs a rejected plane, or substitutes a
previous result.

Centre localization remains bounded and same-frame. A four-pixel annulus grid
and fixed one-pixel refinement supply only a seed. From that seed the Action
collects at most one high-confidence ridge point from each of the same 54
structural directions, performs one algebraic circle fit, applies one fixed
median/MAD residual trim, and refits exactly once. At least 36 inliers are
required. Candidate fitting may use at most 1200 evenly spaced accepted pixels,
and dense planes above 6000 accepted pixels reject explicitly. The fit exposes
its point/inlier counts, residual p95, and centre covariance XX/XY/YY/trace;
covariance trace above 4 square pixels rejects as `CENTER_COVARIANCE_HIGH`.
These steps only choose the centre at which the unchanged structural classifier
runs; none can establish a reticle by itself.

The same fixed coarse grid supplies at most six separated runner-up seeds. Each
uses the identical bounded circle fit. A runner-up at least six pixels from the
selected centre rejects as `MULTIMODAL_CENTER_AMBIGUOUS` only when its inlier
support is within two points and its residual p95 is within 1.5 pixels of the
selected fit. Events are not involved because this is a finite observation;
the output planes expose runner-up centre, support, residual, distance, and
`centerModeCount`. Missing or degenerate fit evidence remains explicit
`UNKNOWN`, never a previous-frame centre.

At the selected centre, every plane runs the same polar thin-ridge classifier.
It searches radii 40–52 pixels. For each radius it subtracts local inner and
outer background from a three-sample radial ridge. It evaluates a true
270-degree arc as 54 five-degree structural bins and deliberately omits the
right-facing target-label quarter. The candidate must satisfy all three hard
Gates:

- at least 36 of 54 structural bins contain a positive thresholded ridge;
- the detected ridge radii have median absolute deviation at most 2 pixels;
- radial-edge orientation coherence is at least 0.59.

No sequential threshold or shape fallback follows a failed Gate. One through
three structural runs is `SOLID`; four or more is `DASHED`. The classifier does
not morphologically close gaps.

`arcConfidencePermille` weights structural coverage at 50%, radius consistency
at 25%, radial-edge orientation coherence at 20%, and centre uniqueness at 5%.
The output also exposes `alphaInvariantOrangeScore`, `structuralCoverage`,
`radiusMAD`, and `orientationCoherence`; legacy topology and confidence fields
remain as compatible diagnostics. Shape confidence, rather than global image
contrast or OCR confidence, selects between viable same-frame adaptive planes.
A distinct near-tied centre or comparable robust circle mode rejects the plane.
If two viable adaptive planes
point to distinct centres with near-equal coverage, the Action returns
`UNKNOWN` rather than silently selecting one.

The default `ADAPTIVE_ORANGE` policy keeps strict RGB diagnostic-only. The
explicit `OCCLUSION_AWARE` policy is reserved for a separately confirmed
`MOVE TO OBTAIN LINE OF SIGHT TO TARGET` workflow. `HUD_OVERLAY_AWARE` is used
by the identity-bound normal visible-target alignment workflow. Under either
explicit policy, strict RGB may be selected only after both adaptive planes
reject the same current ROI. This explicit same-frame fusion rule cannot rescue
missing identity, invalid layout, or an invalid polar three-quarter structure.

This local CV deliberately ignores target identity. A valid `DASHED`
three-quarter ring is a position observation, including for a hyperspace
target; it is not weaker identity evidence and does not itself authorize an FSD
input. Initial acquisition must come from `supercruise-target-position`. After
that, a bounded controller may track the already-identified reticle by feeding
each current result back as the next hint. A tracking miss authorizes no
steering and must transition visibly back to identity acquisition.

The ROI, candidate grids, evidence-plane fusion order, polar Gates, and 64M
Starlark step budget are fixed. Exceeding the declared budget or receiving
incomplete screen evidence is a terminal infrastructure failure, not domain
`UNKNOWN`.
