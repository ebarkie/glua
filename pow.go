package glua

import "math"

// math.Pow computes exp(y*log(x)) in double precision, so its error grows with |y*log(x)|: a
// few units in the last place are common, enough to show in the 14 digits that tostring
// prints. C libraries are much closer to correctly rounded. powPos carries y*log(x) in
// double-double precision, and corrects exp's last-bit error, which brings it to the
// correctly rounded result in all but about one case in a thousand.

const (
	ln2Hi = 6.93147180369123816490e-01 // the top 32 bits of ln 2, so k*ln2Hi is exact
	ln2Lo = 1.90821492927058770002e-10 // ln 2 - ln2Hi
)

// logDD is log(x) for a finite x > 0 as an unevaluated sum hi+lo, good to about 1e-19.
func logDD(x float64) (float64, float64) {
	m, e := math.Frexp(x)
	if m < math.Sqrt2/2 {
		m *= 2
		e--
	}
	// log(m) = 2 atanh(s) = 2(s + s^3/3 + s^5/5 + ...), s = (m-1)/(m+1), |s| < 0.172. The
	// first two terms are carried in double-double; the rest are too small to need it.
	num := m - 1 // exact
	dh := m + 1
	dl := m - (dh - 1) // the rounding error of m+1
	sh := num / dh
	sl := (math.FMA(-sh, dh, num) - sh*dl) / dh
	s2 := sh * sh
	s3h := s2 * sh
	s3l := math.FMA(s2, sh, -s3h) + math.FMA(sh, sh, -s2)*sh
	c3h := s3h / 3
	c3l := (math.FMA(-c3h, 3, s3h)+s3l)/3 + s2*sl // (sh+sl)^3/3 = sh^3/3 + sh^2 sl + ...
	rest := s3h * s2 * (1.0/5 + s2*(1.0/7+s2*(1.0/9+s2*(1.0/11+s2*(1.0/13+s2*(1.0/15+
		s2*(1.0/17+s2*(1.0/19+s2*(1.0/21+s2*(1.0/23+s2/25))))))))))
	h := sh + c3h
	l := (sh - h) + c3h + sl + c3l + rest
	fe := float64(e)
	a := fe * ln2Hi
	lh := a + 2*h
	ll := (a - lh) + 2*h + 2*l + fe*ln2Lo
	return lh + ll, ll - ((lh + ll) - lh)
}

// powPos is x^y for a finite x > 0 and a finite y.
func powPos(x, y float64) float64 {
	lh, ll := logDD(x)
	zh := y * lh
	zl := math.FMA(y, lh, -zh) + y*ll
	if zh > 709.7 || zh < -708.3 {
		return math.Pow(x, y) // overflows, or is near or below the subnormal range
	}
	// x^y = 2^k exp(r), r = z - k ln 2, |r| <= ln 2 / 2
	k := math.Round(zh / math.Ln2)
	kh := k * ln2Hi
	rh := zh - kh
	rl := ((zh - rh) - kh) + zl - k*ln2Lo
	s := math.Exp(rh + rl)
	// exp(r) = s(1 + d) with d = r - log(s)
	sh, sl := logDD(s)
	d := (rh - sh) + (rl - sl)
	return math.Ldexp(s+s*d, int(k))
}
