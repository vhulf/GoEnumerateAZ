# GoEnumerateAZ
A work in progress script for enumerating various Azure resources which could be interesting from a pentesting perspective... surely there are more modes worth implementing!


For now there are only two implemented modes, one which enuemrates deployment templates across an entire authorization scope, and another which enumeartes all custom role assignments which have been applied to both user managed and system managed identities.
Currently requires a JWT token to be procured which is valid for the "management" azure endpoint... AZ CLI token getting implementation has been attempted but goes untested, vhulf theorizes that the token will be for "portal.azure" not "management.azure" so it's likely non-funcitonal.


If you implement more functionanal modes please open a PR! c:

## USAGE

```
go run enumerateAZ.go --help
go run enumerateAZ.go -t "Authorization: Bearer eyBEARERTOKENCONTENT" -mir
go run enumerateAZ.go -t "Bearer eyBEARERTOKENCONTENT" -dt
go run enumerateAZ.go -t "eyBEARERTOKENCONTENT" -dt -mir
```

or to utilize it as an executable...
```
go build enumerateAZ.go
./enumerateAZ --help
./enumerateAZ.go -t "Authorization: Bearer eyBEARERTOKENCONTENT" -mir
./enumerateAZ.go -t "Bearer eyBEARERTOKENCONTENT" -dt
./enumerateAZ.go -t "eyBEARERTOKENCONTENT" -dt -mir
```

## OUTPUT
Output should be easy to grep/sed & awk through for useful information...

### Deployment Templates
TOADD

### Managed Identities Roles
TOADD
