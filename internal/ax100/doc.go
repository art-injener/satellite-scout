// Package ax100 — кадры GOMspace NanoCom AX.100:
//   - Mode 5 (ASM+Golay): открытое синхрослово, длина под Golay(24,12),
//     полезная нагрузка под CCSDS-скремблером и RS(255,223);
//   - Mode RS: весь поток под G3RUH, затем sync, байт длины и RS.
// После RS обычно CSP. Референс: gr-satellites ax100_deframer / u482c_decode.
package ax100
