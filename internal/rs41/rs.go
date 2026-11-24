package rs41

import (
	"errors"
	"fmt"
)

// RS(255,231) для RS41: GF(2^8) poly 0x11D, корни α^0…α^23 (rs1729 / wikiversity).
const (
	rsNN        = 255
	rsParityLen = 24
	rsDataLen   = rsNN - rsParityLen // 231
	rsSymSize   = 8
	rsGFPoly    = 0x11D
	rsFCR       = 0
	rsPrim      = 1
	rsA0        = rsNN
)

var (
	errRSDecode = errors.New("rs41: reed-solomon decode failed")
	rs41codec   = mustInitRS41()
)

type rsCodec struct {
	alphaTo [rsNN + 1]byte
	indexOf [rsNN + 1]byte
	genpoly [rsParityLen + 1]byte // обычные байты генератора (для encode)
	iprim   int
}

func mustInitRS41() *rsCodec {
	c, err := initRS41()
	if err != nil {
		panic(err)
	}
	return c
}

func initRS41() (*rsCodec, error) {
	c := &rsCodec{}
	c.indexOf[0] = rsA0
	c.alphaTo[rsA0] = 0
	sr := 1
	for i := 0; i < rsNN; i++ {
		c.indexOf[sr] = byte(i)
		c.alphaTo[i] = byte(sr)
		sr <<= 1
		if sr&(1<<rsSymSize) != 0 {
			sr ^= rsGFPoly
		}
		sr &= rsNN
	}
	if sr != 1 {
		return nil, errors.New("rs41: RS field polynomial is not primitive")
	}
	iprim := 1
	for iprim%rsPrim != 0 {
		iprim += rsNN
	}
	c.iprim = iprim / rsPrim

	// Генератор (X-α^0)…(X-α^23): индекс 0 — старший коэффициент (моник = 1),
	// как в wikiversity rs_generator_poly / rs1729.
	g := []byte{1}
	for i := 0; i < rsParityLen; i++ {
		root := c.alphaTo[c.modnn(rsFCR*rsPrim+i*rsPrim)]
		ng := make([]byte, len(g)+1)
		for j, v := range g {
			ng[j] ^= v // * X
			ng[j+1] ^= gfMul(c, v, root)
		}
		g = ng
	}
	if len(g) != rsParityLen+1 {
		return nil, fmt.Errorf("rs41: generator length %d", len(g))
	}
	copy(c.genpoly[:], g)
	return c, nil
}

func (c *rsCodec) modnn(x int) int {
	for x >= rsNN {
		x -= rsNN
		x = (x >> rsSymSize) + (x & rsNN)
	}
	return x
}

func gfMul(c *rsCodec, a, b byte) byte {
	if a == 0 || b == 0 {
		return 0
	}
	return c.alphaTo[c.modnn(int(c.indexOf[a])+int(c.indexOf[b]))]
}

// encodeMsg — [данные|чётность], чётность 24 байта (wikiversity).
func encodeMsg(msg []byte) ([]byte, error) {
	if len(msg) == 0 || len(msg) > rsDataLen {
		return nil, fmt.Errorf("rs41: RS data length %d, want 1..%d", len(msg), rsDataLen)
	}
	c := rs41codec
	out := make([]byte, len(msg)+rsParityLen)
	copy(out, msg)
	for i := 0; i < len(msg); i++ {
		coef := out[i]
		if coef == 0 {
			continue
		}
		for j := 1; j <= rsParityLen; j++ {
			out[i+j] ^= gfMul(c, c.genpoly[j], coef)
		}
	}
	res := make([]byte, len(msg)+rsParityLen)
	copy(res, msg)
	copy(res[len(msg):], out[len(msg):])
	return res, nil
}

// decodeBlock — укороченный блок [данные|чётность]; возвращает данные без чётности.
// В decode уходит только укороченный срез (как в libfec): pad — число неявных
// ведущих нулей полного слова длины 255.
func decodeBlock(block []byte) (data []byte, corrected int, err error) {
	n := len(block)
	if n <= rsParityLen || n > rsNN {
		return nil, 0, fmt.Errorf("rs41: RS block length %d", n)
	}
	pad := rsNN - n
	buf := append([]byte(nil), block...)
	corr, err := rs41codec.decode(buf, pad)
	if err != nil {
		return nil, 0, err
	}
	out := make([]byte, n-rsParityLen)
	copy(out, buf[:len(out)])
	return out, corr, nil
}

// decode — порт Phil Karn decode_rs.c (без стираний), как в internal/fec.
func (c *rsCodec) decode(data []byte, pad int) (int, error) {
	if pad < 0 || pad >= rsNN {
		return 0, fmt.Errorf("rs41: RS pad %d", pad)
	}

	var s [rsParityLen]byte
	for i := 0; i < rsParityLen; i++ {
		s[i] = data[0]
	}
	for j := 1; j < rsNN-pad; j++ {
		for i := 0; i < rsParityLen; i++ {
			if s[i] == 0 {
				s[i] = data[j]
			} else {
				s[i] = data[j] ^ c.alphaTo[c.modnn(int(c.indexOf[s[i]])+(rsFCR+i)*rsPrim)]
			}
		}
	}

	synError := byte(0)
	for i := 0; i < rsParityLen; i++ {
		synError |= s[i]
		s[i] = c.indexOf[s[i]]
	}
	if synError == 0 {
		return 0, nil
	}

	var lambda [rsParityLen + 1]byte
	lambda[0] = 1

	var b [rsParityLen + 1]byte
	for i := 0; i <= rsParityLen; i++ {
		b[i] = c.indexOf[lambda[i]]
	}

	r := 0
	el := 0
	for {
		r++
		if r > rsParityLen {
			break
		}
		var discrR byte
		for i := 0; i < r; i++ {
			if lambda[i] != 0 && s[r-i-1] != rsA0 {
				discrR ^= c.alphaTo[c.modnn(int(c.indexOf[lambda[i]])+int(s[r-i-1]))]
			}
		}
		discrR = c.indexOf[discrR]
		if discrR == rsA0 {
			copy(b[1:], b[:rsParityLen])
			b[0] = rsA0
			continue
		}
		var t [rsParityLen + 1]byte
		t[0] = lambda[0]
		for i := 0; i < rsParityLen; i++ {
			if b[i] != rsA0 {
				t[i+1] = lambda[i+1] ^ c.alphaTo[c.modnn(int(discrR)+int(b[i]))]
			} else {
				t[i+1] = lambda[i+1]
			}
		}
		if 2*el <= r-1 {
			el = r - el
			for i := 0; i <= rsParityLen; i++ {
				if lambda[i] == 0 {
					b[i] = rsA0
				} else {
					b[i] = byte(c.modnn(int(c.indexOf[lambda[i]]) - int(discrR) + rsNN))
				}
			}
		} else {
			copy(b[1:], b[:rsParityLen])
			b[0] = rsA0
		}
		lambda = t
	}

	degLambda := 0
	for i := 0; i <= rsParityLen; i++ {
		lambda[i] = c.indexOf[lambda[i]]
		if lambda[i] != rsA0 {
			degLambda = i
		}
	}

	var reg [rsParityLen + 1]byte
	copy(reg[1:], lambda[1:])
	var root, loc [rsParityLen]int
	count := 0
	k := c.iprim - 1
	for i := 1; i <= rsNN; i++ {
		q := byte(1)
		for j := degLambda; j > 0; j-- {
			if reg[j] != rsA0 {
				reg[j] = byte(c.modnn(int(reg[j]) + j))
				q ^= c.alphaTo[reg[j]]
			}
		}
		if q == 0 {
			root[count] = i
			loc[count] = k
			count++
			if count == degLambda {
				break
			}
		}
		k = c.modnn(k + c.iprim)
	}
	if degLambda != count {
		return 0, errRSDecode
	}

	degOmega := degLambda - 1
	var omega [rsParityLen + 1]byte
	for i := 0; i <= degOmega; i++ {
		var tmp byte
		for j := i; j >= 0; j-- {
			if s[i-j] != rsA0 && lambda[j] != rsA0 {
				tmp ^= c.alphaTo[c.modnn(int(s[i-j])+int(lambda[j]))]
			}
		}
		omega[i] = c.indexOf[tmp]
	}

	for j := count - 1; j >= 0; j-- {
		var num1 byte
		for i := degOmega; i >= 0; i-- {
			if omega[i] != rsA0 {
				num1 ^= c.alphaTo[c.modnn(int(omega[i])+i*root[j])]
			}
		}
		num2 := c.alphaTo[c.modnn(root[j]*(rsFCR-1)+rsNN)]
		var den byte
		for i := min(degLambda, rsParityLen-1) &^ 1; i >= 0; i -= 2 {
			if lambda[i+1] != rsA0 {
				den ^= c.alphaTo[c.modnn(int(lambda[i+1])+i*root[j])]
			}
		}
		if num1 != 0 && loc[j] >= pad {
			data[loc[j]-pad] ^= c.alphaTo[c.modnn(int(c.indexOf[num1])+int(c.indexOf[num2])+rsNN-int(c.indexOf[den]))]
		}
	}
	return count, nil
}
