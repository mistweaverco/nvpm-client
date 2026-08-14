; (#match? @foo "abc+")
(#match? @foo "text ; not comment")
(#eq? @foo "#match?")
(#match? @foo "\\vfoo+")
