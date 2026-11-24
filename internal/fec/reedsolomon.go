package fec

import (
	"errors"
	"fmt"
)

// Параметры RS(255,223) как у Phil Karn libfec encode_rs_8 / decode_rs_8
// (обычный базис). AX.100 в gr-satellites использует именно их.
const (
	RSNN        = 255
	RSParityLen = 32
	RSDataLen   = RSNN - RSParityLen // 223
	rsSymSize   = 8
	rsGFPoly    = 0x187
	rsFCR       = 112
	rsPrim      = 11
	rsA0        = RSNN // ноль в индексной форме
)

// ErrRSDecode — код Рида–Соломона не исправлен.
var ErrRSDecode = errors.New("fec: reed-solomon decode failed")

type rsCodec struct {
	alphaTo [RSNN + 1]byte
	indexOf [RSNN + 1]byte
	genpoly [RSParityLen + 1]byte
	iprim   int
}

var rsCCSDS = mustInitRS()

func mustInitRS() *rsCodec {
	c, err := initRS()
	if err != nil {
		panic(err)
	}
	return c
}

func initRS() (*rsCodec, error) {
	c := &rsCodec{}
	c.indexOf[0] = rsA0
	c.alphaTo[rsA0] = 0
	sr := 1
	for i := 0; i < RSNN; i++ {
		c.indexOf[sr] = byte(i)
		c.alphaTo[i] = byte(sr)
		sr <<= 1
		if sr&(1<<rsSymSize) != 0 {
			sr ^= rsGFPoly
		}
		sr &= RSNN
	}
	if sr != 1 {
		return nil, errors.New("fec: RS field polynomial is not primitive")
	}

	iprim := 1
	for iprim%rsPrim != 0 {
		iprim += RSNN
	}
	c.iprim = iprim / rsPrim

	c.genpoly[0] = 1
	root := rsFCR * rsPrim
	for i := 0; i < RSParityLen; i++ {
		c.genpoly[i+1] = 1
		for j := i; j > 0; j-- {
			if c.genpoly[j] != 0 {
				c.genpoly[j] = c.genpoly[j-1] ^ c.alphaTo[c.modnn(int(c.indexOf[c.genpoly[j]])+root)]
			} else {
				c.genpoly[j] = c.genpoly[j-1]
			}
		}
		c.genpoly[0] = c.alphaTo[c.modnn(int(c.indexOf[c.genpoly[0]])+root)]
		root += rsPrim
	}
	for i := 0; i <= RSParityLen; i++ {
		c.genpoly[i] = c.indexOf[c.genpoly[i]]
	}
	return c, nil
}

func (c *rsCodec) modnn(x int) int {
	for x >= RSNN {
		x -= RSNN
		x = (x >> rsSymSize) + (x & RSNN)
	}
	return x
}

// EncodeRS считает 32 байта чётности для блока данных длиной 1..223.
func EncodeRS(data []byte) ([]byte, error) {
	if len(data) == 0 || len(data) > RSDataLen {
		return nil, fmt.Errorf("fec: RS data length %d, want 1..%d", len(data), RSDataLen)
	}
	pad := RSDataLen - len(data)
	parity := make([]byte, RSParityLen)
	c := rsCCSDS
	for i := 0; i < RSDataLen-pad; i++ {
		feedback := c.indexOf[data[i]^parity[0]]
		if feedback != rsA0 {
			for j := 1; j < RSParityLen; j++ {
				parity[j] ^= c.alphaTo[c.modnn(int(feedback)+int(c.genpoly[RSParityLen-j]))]
			}
		}
		copy(parity[0:], parity[1:])
		if feedback != rsA0 {
			parity[RSParityLen-1] = c.alphaTo[c.modnn(int(feedback)+int(c.genpoly[0]))]
		} else {
			parity[RSParityLen-1] = 0
		}
	}
	return parity, nil
}

// DecodeRS принимает блок [данные|чётность] длиной 33..255.
// Возвращает исправленные данные без чётности и число исправленных байт.
func DecodeRS(block []byte) (data []byte, corrected int, err error) {
	n := len(block)
	if n <= RSParityLen || n > RSNN {
		return nil, 0, fmt.Errorf("fec: RS block length %d, want %d..%d", n, RSParityLen+1, RSNN)
	}
	pad := RSNN - n
	// Работаем на копии укороченного кодового слова без ведущего pad:
	// decode_rs_8 правит data[loc-pad], то есть индексы относительно блока без pad.
	buf := append([]byte(nil), block...)
	corr, err := rsCCSDS.decode(buf, pad)
	if err != nil {
		return nil, 0, err
	}
	out := make([]byte, n-RSParityLen)
	copy(out, buf[:len(out)])
	return out, corr, nil
}

// decode — порт Phil Karn decode_rs.c (без стираний), LGPL libfec.
func (c *rsCodec) decode(data []byte, pad int) (int, error) {
	if pad < 0 || pad >= RSNN {
		return 0, fmt.Errorf("fec: RS pad %d", pad)
	}

	var s [RSParityLen]byte
	for i := 0; i < RSParityLen; i++ {
		s[i] = data[0]
	}
	for j := 1; j < RSNN-pad; j++ {
		for i := 0; i < RSParityLen; i++ {
			if s[i] == 0 {
				s[i] = data[j]
			} else {
				s[i] = data[j] ^ c.alphaTo[c.modnn(int(c.indexOf[s[i]])+(rsFCR+i)*rsPrim)]
			}
		}
	}

	synError := byte(0)
	for i := 0; i < RSParityLen; i++ {
		synError |= s[i]
		s[i] = c.indexOf[s[i]]
	}
	if synError == 0 {
		return 0, nil
	}

	var lambda [RSParityLen + 1]byte
	lambda[0] = 1

	var b [RSParityLen + 1]byte
	for i := 0; i <= RSParityLen; i++ {
		b[i] = c.indexOf[lambda[i]]
	}

	r := 0
	el := 0
	for {
		r++
		if r > RSParityLen {
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
			copy(b[1:], b[:RSParityLen])
			b[0] = rsA0
			continue
		}
		var t [RSParityLen + 1]byte
		t[0] = lambda[0]
		for i := 0; i < RSParityLen; i++ {
			if b[i] != rsA0 {
				t[i+1] = lambda[i+1] ^ c.alphaTo[c.modnn(int(discrR)+int(b[i]))]
			} else {
				t[i+1] = lambda[i+1]
			}
		}
		if 2*el <= r-1 {
			el = r - el
			for i := 0; i <= RSParityLen; i++ {
				if lambda[i] == 0 {
					b[i] = rsA0
				} else {
					b[i] = byte(c.modnn(int(c.indexOf[lambda[i]]) - int(discrR) + RSNN))
				}
			}
		} else {
			copy(b[1:], b[:RSParityLen])
			b[0] = rsA0
		}
		lambda = t
	}

	degLambda := 0
	for i := 0; i <= RSParityLen; i++ {
		lambda[i] = c.indexOf[lambda[i]]
		if lambda[i] != rsA0 {
			degLambda = i
		}
	}

	var reg [RSParityLen + 1]byte
	copy(reg[1:], lambda[1:])
	var root, loc [RSParityLen]int
	count := 0
	k := c.iprim - 1
	for i := 1; i <= RSNN; i++ {
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
		return 0, ErrRSDecode
	}

	degOmega := degLambda - 1
	var omega [RSParityLen + 1]byte
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
		num2 := c.alphaTo[c.modnn(root[j]*(rsFCR-1)+RSNN)]
		var den byte
		for i := min(degLambda, RSParityLen-1) &^ 1; i >= 0; i -= 2 {
			if lambda[i+1] != rsA0 {
				den ^= c.alphaTo[c.modnn(int(lambda[i+1])+i*root[j])]
			}
		}
		if num1 != 0 && loc[j] >= pad {
			data[loc[j]-pad] ^= c.alphaTo[c.modnn(int(c.indexOf[num1])+int(c.indexOf[num2])+RSNN-int(c.indexOf[den]))]
		}
	}
	return count, nil
}
