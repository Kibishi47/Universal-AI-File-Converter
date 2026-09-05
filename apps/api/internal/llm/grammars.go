package llm

// GBNF grammar definitions for strict structured llama.cpp decoding

// GrammarFeasibility forces strict JSON: { "convertible": bool, "reason": string }
const GrammarFeasibility = `root ::= "{" ws "\"convertible\":" ws boolean "," ws "\"reason\":" ws string "}"
boolean ::= "true" | "false"
ws ::= [ \t\n]*
string ::= "\"" [^"\\]* "\""`

// GrammarExplanation forces strict JSON: { "convertible": false, "reason": short_string } with reason restricted to max 120 chars
const GrammarExplanation = `root ::= "{" ws "\"convertible\":" ws "false" "," ws "\"reason\":" ws short_string "}"
ws ::= [ \t\n]*
short_string ::= "\"" [^"\\]{1,120} "\""`

// GrammarTools forces JSON: { "tools": string[], "package_hint": string }
const GrammarTools = `root ::= "{" ws "\"tools\":" ws tool_list "," ws "\"package_hint\":" ws string "}"
tool_list ::= "[" ws (string ("," ws string)*)? ws "]"
ws ::= [ \t\n]*
string ::= "\"" [a-zA-Z0-9_.-]+ "\""`

// GrammarExecution forces JSON: { "steps": [ { "command": string, "args": string[] } ] }
const GrammarExecution = `root ::= "{" ws "\"steps\":" ws step_list "}"
step_list ::= "[" ws step ("," ws step)* ws "]"
step ::= "{" ws "\"command\":" ws string "," ws "\"args\":" ws string_list "}"
string_list ::= "[" ws (string ("," ws string)*)? ws "]"
ws ::= [ \t\n]*
string ::= "\"" [^"\\]* "\""`
