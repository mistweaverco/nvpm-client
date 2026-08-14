((identifier) @foo
 (#match? @foo "^foo$"))
((identifier) @bar
 (#match? @bar "\\vfoo+"))
((identifier) @baz
 (#not-match? @baz "\\v(foo|bar)"))
