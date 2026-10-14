package glua

// Instructions are 32 bits with byte-aligned fields: op:8 A:8 B:8 C:8, or op:8 A:8 Bx:16.
// Byte-wide operands index a frame viewed as *[256]Value with no bounds check. Some
// instructions are followed by one auxiliary word.
//
// This is the spike's instruction set: enough for the benchmarks, the boundary and the
// continuation-frame machinery. The compiler of milestone 1 extends it.
const (
	opBad       = iota // never emitted; also the padding word after the last instruction
	opMove             // R[A] = R[B]
	opLoadK            // R[A] = K[Bx]
	opLoadI            // R[A] = int16(Bx)
	opLoadNil          // R[A], ..., R[A+B] = nil
	opLoadFalse        // R[A] = false
	opLoadTrue         // R[A] = true
	opGetUpval         // R[A] = Up[B]
	opGetField         // R[A] = R[B][K[C]]; aux: hint site
	opSetField         // R[A][K[B]] = R[C]; aux: hint site
	opNewTable         // R[A] = {}; B: array size hint, C: hash size hint
	opAdd              // R[A] = R[B] + R[C]
	opSub              // R[A] = R[B] - R[C]
	opMul              // R[A] = R[B] * R[C]
	opAddK             // R[A] = R[B] + K[C]
	opSubK             // R[A] = R[B] - K[C]
	opMulK             // R[A] = R[B] * K[C]
	opJmp              // pc += int16(Bx)
	opLtJmp            // if R[A] < R[B] fall through, else pc += int32(aux)
	opLtKJmp           // if R[A] < K[B] fall through, else pc += int32(aux)
	opEqKJmp           // if R[A] == K[B] fall through, else pc += int32(aux)
	opForPrep          // prepare R[A..A+3]; skip the loop, pc += Bx, when it runs no times
	opForLoop          // count down; while iterations remain, R[A+3] = next, pc -= Bx
	opCall             // R[A], ..., R[A+C-2] = R[A](R[A+1], ..., R[A+B-1]); B=0: args to top; C=0: results to top
	opReturn           // return R[A], ..., R[A+B-2]; B=0: up to top
	opReturn0          // return
	opReturn1          // return R[A]
	opClosure          // R[A] = closure(P[Bx])
	numOps
)

// hasAux reports whether an opcode is followed by an auxiliary word.
var hasAux = [256]bool{
	opGetField: true,
	opSetField: true,
	opLtJmp:    true,
	opLtKJmp:   true,
	opEqKJmp:   true,
}

var opNames = [numOps]string{
	"BAD", "MOVE", "LOADK", "LOADI", "LOADNIL", "LOADFALSE", "LOADTRUE", "GETUPVAL", "GETFIELD",
	"SETFIELD", "NEWTABLE", "ADD", "SUB", "MUL", "ADDK", "SUBK", "MULK", "JMP", "LTJMP", "LTKJMP",
	"EQKJMP", "FORPREP", "FORLOOP", "CALL", "RETURN", "RETURN0", "RETURN1", "CLOSURE",
}

func opName(op uint8) string {
	if int(op) < len(opNames) {
		return opNames[op]
	}
	return "BAD"
}

func iABC(op, a, b, c uint8) uint32      { return uint32(op) | uint32(a)<<8 | uint32(b)<<16 | uint32(c)<<24 }
func iABx(op, a uint8, bx uint16) uint32 { return uint32(op) | uint32(a)<<8 | uint32(bx)<<16 }

func decA(i uint32) int  { return int(uint8(i >> 8)) }
func decB(i uint32) int  { return int(uint8(i >> 16)) }
func decC(i uint32) int  { return int(uint8(i >> 24)) }
func decBx(i uint32) int { return int(i >> 16) }

// exit is why fast returned to the driver.
type exit uint8

const (
	exBadOp      exit = iota // an opcode the loop does not know
	exSlowOp                 // the instruction at pc-1 needs the driver
	exReturn                 // a frame the VM started itself has returned: goEntry or cont
	exResults                // a call returned one result inline, but its caller wanted another count
	exHostCall               // the CALL at pc-1 calls a Go function
	exGrowStack              // the CALL at pc-1 needs a larger register stack
	exGrowFrames             // the CALL at pc-1 needs a larger frame list
	exTick                   // the tick counter expired
)
