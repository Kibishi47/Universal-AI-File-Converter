package llm

// GBNF grammar definitions for strict structured llama.cpp decoding

// GrammarFeasibility forces strict JSON: { "convertible": bool, "reason": string }
const GrammarFeasibility = `root ::= "{" ws "\"convertible\":" ws boolean "," ws "\"reason\":" ws string "}"
boolean ::= "true" | "false"
ws ::= [ \t\n]*
string ::= "\"" [^"\\]* "\""`

// GrammarTools forces JSON: { "required_tools": string[], "alternatives": string[] }
const GrammarTools = `root ::= "{" ws "\"required_tools\":" ws stringlist "," ws "\"alternatives\":" ws stringlist "}"
stringlist ::= "[" ws (string ("," ws string)*)? ws "]"
string ::= "\"" ([^"\\] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
ws ::= [ \t\n\r]*`

// GrammarExecution forces JSON: { "steps": [ { "command": string, "args": string[] } ] }
const GrammarExecution = `root ::= "{" ws "\"steps\":" ws steplist "}"
steplist ::= "[" ws (step ("," ws step)*)? ws "]"
step ::= "{" ws "\"command\":" ws string "," ws "\"args\":" ws stringlist "}"
stringlist ::= "[" ws (string ("," ws string)*)? ws "]"
string ::= "\"" ([^"\\] | "\\" (["\\/bfnrt] | "u" [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F] [0-9a-fA-F]))* "\""
ws ::= [ \t\n\r]*`
