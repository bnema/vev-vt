# Compatibility fixtures

`vevs-v4-restore.golden.hex` is an opaque VEVS v4 restore envelope retained for
vev consumer compatibility. The standalone module does not decode or own the
VEVS outer format; its fixture test verifies the envelope header, length, CRC,
and embedded VTH3 payload markers only. The vev repository owns full VEVS restore
semantics.
