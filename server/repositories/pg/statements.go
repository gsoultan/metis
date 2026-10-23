package pg

// The store's root package registers every statement in model.Queries, the
// text the generator PREPAREd, in its init. Nothing here names it otherwise,
// and without the import none of those statements would run: storm refuses a
// declared statement it was not shown at generate time.
import _ "github.com/gsoultan/metis/server/repositories/store"
