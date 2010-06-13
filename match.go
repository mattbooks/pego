package main

import (
   "fmt"
   "container/vector"
   "strings"
   "os"
)

type StackEntry struct {
   p, i, c int
}

type Stack struct { *vector.Vector }
func (s *Stack) String() string {
   ret := new(vector.StringVector)
   ret.Push("[")
   for v := range s.Iter() {
      switch v := v.(type) {
      case *StackEntry: ret.Push(fmt.Sprintf("%v", *v))
      default: ret.Push(fmt.Sprintf("%v", v))
      }
   }
   ret.Push("]")
   return strings.Join(ret.Data(), " ")
}

type CaptureEntry struct {
   p, start, end int
   handler CaptureHandler
   value interface{}
}

type CapStack struct {
   data []*CaptureEntry
   top int
}

func NewCapStack() *CapStack {
   return &CapStack{}
}

func (s *CapStack) String() string {
   ret := new(vector.StringVector)
   ret.Push("[")
   var i int
   for i = 0; i < s.top; i++ {
      ret.Push(fmt.Sprintf("%v", s.data[i]))
   }
   ret.Push("<|")
   for ; i < len(s.data); i++ {
      ret.Push(fmt.Sprintf("%v", s.data[i]))
   }
   ret.Push("]")
   return strings.Join(ret.Data(), " ")
}

func (s *CapStack) Open(p int, start int) *CaptureEntry {
   if s.data == nil {
      s.data = make([]*CaptureEntry,8)
   } else if len(s.data) == s.top {
      newData := make([]*CaptureEntry,2*len(s.data)+1)
      copy(newData, s.data)
      s.data = newData
   }
   s.data[s.top] = &CaptureEntry{p:p,start:start,end:-1}
   s.top++
   return s.data[s.top-1]
}

func (s *CapStack) Close(end int) (*CaptureEntry,int) {
   var i int
   for i = s.top-1; i >= 0; i-- {
      if s.data[i].end == -1 {
         s.data[i].end = end
         return s.data[i], s.top - i - 1
      }
   }
   return nil, 0
}

type CaptureResult struct {
   start, end int
   value interface{}
}

func (s *CapStack) Pop(count int) []*CaptureResult {
   subcaps := make([]*CaptureResult,count)
   i := s.top - count
   for j := 0; j < count; j++ {
      subcaps[j] = &CaptureResult{s.data[i+j].start,s.data[i+j].end,s.data[i+j].value}
   }
   s.top -= count
   return subcaps
}

func (s *CapStack) Mark() int {
   return s.top
}

func (s *CapStack) Rollback(mark int) {
   s.top = mark
}



func match(program []Instruction, input string) (interface{},os.Error,int) {
   const FAIL = -1
   var p, i, c int
   stack := &Stack{new(vector.Vector)}
   captures := NewCapStack()
   for p = 0; p < len(program); {
      if p == FAIL {
         if stack.Len() == 0 {
            return nil, os.ErrorString("Stack is empty"),i
         }
         switch e := stack.Pop().(type) {
         case *StackEntry:
            p, i, c = e.p, e.i, e.c
            captures.Rollback(c)
         case int:
         }
         continue
      }
      //fmt.Printf("%6d  %s\n", p, program[p])
      switch op := program[p].(type) {
      default:
         return nil, os.ErrorString(fmt.Sprintf("Unimplemented: %#v", program[p])), i
      case nil: p++
      case *Char:
         if i < len(input) && input[i] == op.char {
            p++
            i++
         } else {
            p = FAIL
         }
      case *Charset:
         if i < len(input) && op.Has(input[i]) {
            p++
            i++
         } else {
            p = FAIL
         }
      case *Any:
         if i + op.count > len(input) {
            p = FAIL
         } else {
            p++
            i += op.count
         }
      case *Jump:
         p += op.offset
      case *Choice:
         stack.Push(&StackEntry{p+op.offset,i,captures.Mark()})
         p++
      case *Call:
         stack.Push(p+1)
         p += op.offset
      case *Return:
         if stack.Len() == 0 {
            return nil, os.ErrorString("Return with empty stack"),i
         }
         switch e := stack.Pop().(type) {
         case *StackEntry:
            return nil, os.ErrorString("Expecting return address on stack; Found failure address"),i
         case int:
            p = e
         }
      case *Commit:
         if stack.Len() == 0 {
            return nil, os.ErrorString("Commit with empty stack"),i
         }
         switch stack.Pop().(type) {
         case *StackEntry:
            p += op.offset
         case int:
            return nil, os.ErrorString("Expecting failure address on stack; Found return address"),i
         }
      case *OpenCapture:
         e := captures.Open(p, i - op.capOffset)
         if op.handler == nil {
            e.handler = &SimpleCapture{}
         } else {
            e.handler = op.handler
         }
         p++
      case *CloseCapture:
         e, count := captures.Close(i - op.capOffset)
         v, err := e.handler.Process(input,e.start,e.end,captures,count)
         if err != nil { return nil, err, i }
         e.value = v
         p++
      case *FullCapture:
         e := captures.Open(p, i - op.capOffset)
         captures.Close(i)
         v, err := e.handler.Process(input,e.start,e.end,captures,0)
         if err != nil { return nil, err, i }
         e.value = v
         p++
      case *EmptyCapture:
         e := captures.Open(p, i - op.capOffset)
         captures.Close(i - op.capOffset)
         v, err := e.handler.Process(input,e.start,e.end,captures,0)
         if err != nil { return nil, err, i }
         e.value = v
         p++
      case *Fail:
         p = FAIL
      case *End:
         caps := captures.Pop(captures.top)
         var ret interface{}
         if len(caps) > 0 && caps[0] != nil { ret = caps[0].value }
         return ret, nil, i
      }
   }
   return nil, os.ErrorString("Invalid jump or missing End instruction."), i
}

func main() {
   instr := []Instruction{
/*  0   */ &Call{+2},   // --v A
/*  1   */ &Jump{+23},  // --v E

/*  2 A */ &OpenCapture{0,&ListCapture{}},
/*  3   */ &Call{+3}, // --v B
/*  4   */ &CloseCapture{0},
/*  5   */ &Return{},

/*  6 B */ &OpenCapture{0,&SimpleCapture{}},
/*  7 a */ &Choice{+3}, // --v b
/*  8   */ &Charset{[8]uint32{ // [^()]
              0xFFFFFFFF, 0xFFFFFCFF, 0xFFFFFFFF, 0xFFFFFFFF,
              0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF}},
/*  9   */ &Commit{-2}, // --^ a ^
/* 10 b */ &Choice{+6}, // --v d v
/* 11   */ &Call{+7},   // --v C v
/* 12 c */ &Choice{+3}, // --v d v
/* 13   */ &Charset{[8]uint32{ // [^()]
              0xFFFFFFFF, 0xFFFFFCFF, 0xFFFFFFFF, 0xFFFFFFFF,
              0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF, 0xFFFFFFFF}},
/* 14   */ &Commit{-2}, // --^ c ^
/* 15 d */ &Commit{-5}, // --^ b ^
/* 16   */ &CloseCapture{0},
/* 17   */ &Return{},

/* 18 C */ &Char{'('},
/* 19   */ &OpenCapture{0,&ListCapture{}},
/* 20   */ &Call{-14},  // --^ B
/* 21   */ &CloseCapture{0},
/* 22   */ &Char{')'},
/* 23   */ &Return{},

/* 24 E */ &End{},
   }
   tests := []string{
      "x", "(x)", "a(b(c)d(e)f)g", ")",
   }
   for _, s := range tests {
      fmt.Printf("\n\n=== MATCHING %q ===\n", s)
      r,err,pos := match(instr, s)
      if r != nil { fmt.Printf("r = %T: %v\n", r, r) }
      if err != nil { fmt.Printf("err = %#v\n", err) }
      fmt.Printf("pos = %d\n", pos)
      if pos != len(s) {
         fmt.Println("Failed to match whole input")
      }
   }
}
