((identifier) @foo
 (#match? @foo "^foo$"))
((identifier) @bar
 (#match? @bar "foo+"))
((identifier) @baz
 (#not-match? @baz "(foo|bar)"))
