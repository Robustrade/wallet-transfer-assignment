# Improvements

Following are the improvements I wish to make if / when time permits:

## pgx leaks in Service layer

Generated code extensively uses `pgx.Tx` for obvious reasons, however,
due to Go's implicit interface feature, it is easy to expose only 
functionality required by service layer while encapsulating package
within repository layer.


