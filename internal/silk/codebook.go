package silk

var (
	// In definition of codebook 'a = 0, b = 1...'

	//   +----+---------------------+
	//   | I1 | Coefficient         |
	//   +----+---------------------+
	//   |    | 0 1 2 3 4 5 6 7 8 9 |
	//   | 0  | a a a a a a a a a a |
	//   |    |                     |
	//   | 1  | b d b c c b c b b b |
	//   |    |                     |
	//   | 2  | c b b b b b b b b b |
	//   |    |                     |
	//   | 3  | b c c c c b c b b b |
	//   |    |                     |
	//   | 4  | c d d d d c c c c c |
	//   |    |                     |
	//   | 5  | a f d d c c c c b b |
	//   |    |                     |
	//   | g  | a c c c c c c c c b |
	//   |    |                     |
	//   | 7  | c d g e e e f e f f |
	//   |    |                     |
	//   | 8  | c e f f e f e g e e |
	//   |    |                     |
	//   | 9  | c e e h e f e f f e |
	//   |    |                     |
	//   | 10 | e d d d c d c c c c |
	//   |    |                     |
	//   | 11 | b f f g e f e f f f |
	//   |    |                     |
	//   | 12 | c h e g f f f f f f |
	//   |    |                     |
	//   | 13 | c h f f f f f g f e |
	//   |    |                     |
	//   | 14 | d d f e e f e f e e |
	//   |    |                     |
	//   | 15 | c d d f f e e e e e |
	//   |    |                     |
	//   | 16 | c e e g e f e f f f |
	//   |    |                     |
	//   | 17 | c f e g f f f e f e |
	//   |    |                     |
	//   | 18 | c h e f e f e f f f |
	//   |    |                     |
	//   | 19 | c f e g h g f g f e |
	//   |    |                     |
	//   | 20 | d g h e g f f g e f |
	//   |    |                     |
	//   | 21 | c h g e e e f e f f |
	//   |    |                     |
	//   | 22 | e f f e g g f g f e |
	//   |    |                     |
	//   | 23 | c f f g f g e g e e |
	//   |    |                     |
	//   | 24 | e f f f d h e f f e |
	//   |    |                     |
	//   | 25 | c d e f f g e f f e |
	//   |    |                     |
	//   | 26 | c d c d d e c d d d |
	//   |    |                     |
	//   | 27 | b b c c c c c d c c |
	//   |    |                     |
	//   | 28 | e f f g g g f g e f |
	//   |    |                     |
	//   | 29 | d f f e e e e d d c |
	//   |    |                     |
	//   | 30 | c f d h f f e e f e |
	//   |    |                     |
	//   | 31 | e e f e f g f g f e |
	//   +----+---------------------+
	//   Table 17: Codebook Selection for NB/MB Normalized LSF Stage-2 Index
	//             Decoding
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
	codebookNormalizedLSFStageTwoIndexNarrowbandOrMediumband = [][]uint{
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{1, 3, 1, 2, 2, 1, 2, 1, 1, 1},
		{2, 1, 1, 1, 1, 1, 1, 1, 1, 1},
		{1, 2, 2, 2, 2, 1, 2, 1, 1, 1},
		{2, 3, 3, 3, 3, 2, 2, 2, 2, 2},
		{0, 5, 3, 3, 2, 2, 2, 2, 1, 1},
		{0, 2, 2, 2, 2, 2, 2, 2, 2, 1},
		{2, 3, 6, 4, 4, 4, 5, 4, 5, 5},
		{2, 4, 5, 5, 4, 5, 4, 6, 4, 4},
		{2, 4, 4, 7, 4, 5, 4, 5, 5, 4},
		{4, 3, 3, 3, 2, 3, 2, 2, 2, 2},
		{1, 5, 5, 6, 4, 5, 4, 5, 5, 5},
		{2, 7, 4, 6, 5, 5, 5, 5, 5, 5},
		{2, 7, 5, 5, 5, 5, 5, 6, 5, 4},
		{3, 3, 5, 4, 4, 5, 4, 5, 4, 4},
		{2, 3, 3, 5, 5, 4, 4, 4, 4, 4},
		{2, 4, 4, 6, 4, 5, 4, 5, 5, 5},
		{2, 5, 4, 6, 5, 5, 5, 4, 5, 4},
		{2, 7, 4, 5, 4, 5, 4, 5, 5, 5},
		{2, 5, 4, 6, 7, 6, 5, 6, 5, 4},
		{3, 6, 7, 4, 6, 5, 5, 6, 4, 5},
		{2, 7, 6, 4, 4, 4, 5, 4, 5, 5},
		{4, 5, 5, 4, 6, 6, 5, 6, 5, 4},
		{2, 5, 5, 6, 5, 6, 4, 6, 4, 4},
		{4, 5, 5, 5, 3, 7, 4, 5, 5, 4},
		{2, 3, 4, 5, 5, 6, 4, 5, 5, 4},
		{2, 3, 2, 3, 3, 4, 2, 3, 3, 3},
		{1, 1, 2, 2, 2, 2, 2, 3, 2, 2},
		{4, 5, 5, 6, 6, 6, 5, 6, 4, 5},
		{3, 5, 5, 4, 4, 4, 4, 3, 3, 2},
		{2, 5, 3, 7, 5, 5, 4, 4, 5, 4},
		{4, 4, 5, 4, 5, 6, 5, 6, 5, 4},
	}

	//  +----+------------------------------------------------+
	//  | I1 | Coefficient                                    |
	//  +----+------------------------------------------------+
	//  |    | 0  1  2  3  4  5  6  7  8  9 10 11 12 13 14 15 |
	//  |    |                                                |
	//  | 0  | i  i  i  i  i  i  i  i  i  i  i  i  i  i  i  i |
	//  |    |                                                |
	//  | 1  | k  l  l  l  l  l  k  k  k  k  k  j  j  j  i  l |
	//  |    |                                                |
	//  | 2  | k  n  n  l  p  m  m  n  k  n  m  n  n  m  l  l |
	//  |    |                                                |
	//  | 3  | i  k  j  k  k  j  j  j  j  j  i  i  i  i  i  j |
	//  |    |                                                |
	//  | 4  | i  o  n  m  o  m  p  n  m  m  m  n  n  m  m  l |
	//  |    |                                                |
	//  | 5  | i  l  n  n  m  l  l  n  l  l  l  l  l  l  k  m |
	//  |    |                                                |
	//  | 6  | i  i  i  i  i  i  i  i  i  i  i  i  i  i  i  i |
	//  |    |                                                |
	//  | 7  | i  k  o  l  p  k  n  l  m  n  n  m  l  l  k  l |
	//  |    |                                                |
	//  | 8  | i  o  k  o  o  m  n  m  o  n  m  m  n  l  l  l |
	//  |    |                                                |
	//  | 9  | k  j  i  i  i  i  i  i  i  i  i  i  i  i  i  i |
	//  |    |                                                |
	//  | 10 | i  j  i  i  i  i  i  i  i  i  i  i  i  i  i  j |
	//  |    |                                                |
	//  | 11 | k  k  l  m  n  l  l  l  l  l  l  l  k  k  j  l |
	//  |    |                                                |
	//  | 12 | k  k  l  l  m  l  l  l  l  l  l  l  l  k  j  l |
	//  |    |                                                |
	//  | 13 | l  m  m  m  o  m  m  n  l  n  m  m  n  m  l  m |
	//  |    |                                                |
	//  | 14 | i  o  m  n  m  p  n  k  o  n  p  m  m  l  n  l |
	//  |    |                                                |
	//  | 15 | i  j  i  j  j  j  j  j  j  j  i  i  i  i  j  i |
	//  |    |                                                |
	//  | 16 | j  o  n  p  n  m  n  l  m  n  m  m  m  l  l  m |
	//  |    |                                                |
	//  | 17 | j  l  l  m  m  l  l  n  k  l  l  n  n  n  l  m |
	//  |    |                                                |
	//  | 18 | k  l  l  k  k  k  l  k  j  k  j  k  j  j  j  m |
	//  |    |                                                |
	//  | 19 | i  k  l  n  l  l  k  k  k  j  j  i  i  i  i  i |
	//  |    |                                                |
	//  | 20 | l  m  l  n  l  l  k  k  j  j  j  j  j  k  k  m |
	//  |    |                                                |
	//  | 21 | k  o  l  p  p  m  n  m  n  l  n  l  l  k  l  l |
	//  |    |                                                |
	//  | 22 | k  l  n  o  o  l  n  l  m  m  l  l  l  l  k  m |
	//  |    |                                                |
	//  | 23 | j  l  l  m  m  m  m  l  n  n  n  l  j  j  j  j |
	//  |    |                                                |
	//  | 24 | k  n  l  o  o  m  p  m  m  n  l  m  m  l  l  l |
	//  |    |                                                |
	//  | 25 | i  o  j  j  i  i  i  i  i  i  i  i  i  i  i  i |
	//  |    |                                                |
	//  | 26 | i  o  o  l  n  k  n  n  l  m  m  p  p  m  m  m |
	//  |    |                                                |
	//  | 27 | l  l  p  l  n  m  l  l  l  k  k  l  l  l  k  l |
	//  |    |                                                |
	//  | 28 | i  i  j  i  i  i  k  j  k  j  j  k  k  k  j  j |
	//  |    |                                                |
	//  | 29 | i  l  k  n  l  l  k  l  k  j  i  i  j  i  i  j |
	//  |    |                                                |
	//  | 30 | l  n  n  m  p  n  l  l  k  l  k  k  j  i  j  i |
	//  |    |                                                |
	//  | 31 | k  l  n  l  m  l  l  l  k  j  k  o  m  i  i  i |
	//  +----+------------------------------------------------+
	//  Table 18: Codebook Selection for WB Normalized LSF Stage-2 Index
	//            Decoding
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
	codebookNormalizedLSFStageTwoIndexWideband = [][]uint{
		{8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8},
		{10, 11, 11, 11, 11, 11, 10, 10, 10, 10, 10, 9, 9, 9, 8, 11},
		{10, 13, 13, 11, 15, 12, 12, 13, 10, 13, 12, 13, 13, 12, 11, 11},
		{8, 10, 9, 10, 10, 9, 9, 9, 9, 9, 8, 8, 8, 8, 8, 9},
		{8, 14, 13, 12, 14, 12, 15, 13, 12, 12, 12, 13, 13, 12, 12, 11},
		{8, 11, 13, 13, 12, 11, 11, 13, 11, 11, 11, 11, 11, 11, 10, 12},
		{8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8},
		{8, 10, 14, 11, 15, 10, 13, 11, 12, 13, 13, 12, 11, 11, 10, 11},
		{8, 14, 10, 14, 14, 12, 13, 12, 14, 13, 12, 12, 13, 11, 11, 11},
		{10, 9, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8},
		{8, 9, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 9},
		{10, 10, 11, 12, 13, 11, 11, 11, 11, 11, 11, 11, 10, 10, 9, 11},
		{10, 10, 11, 11, 12, 11, 11, 11, 11, 11, 11, 11, 11, 10, 9, 11},
		{11, 12, 12, 12, 14, 12, 12, 13, 11, 13, 12, 12, 13, 12, 11, 12},
		{8, 14, 12, 13, 12, 15, 13, 10, 14, 13, 15, 12, 12, 11, 13, 11},
		{8, 9, 8, 9, 9, 9, 9, 9, 9, 9, 8, 8, 8, 8, 9, 8},
		{9, 14, 13, 15, 13, 12, 13, 11, 12, 13, 12, 12, 12, 11, 11, 12},
		{9, 11, 11, 12, 12, 11, 11, 13, 10, 11, 11, 13, 13, 13, 11, 12},
		{10, 11, 11, 10, 10, 10, 11, 10, 9, 10, 9, 10, 9, 9, 9, 12},
		{8, 10, 11, 13, 11, 11, 10, 10, 10, 9, 9, 8, 8, 8, 8, 8},
		{11, 12, 11, 13, 11, 11, 10, 10, 9, 9, 9, 9, 9, 10, 10, 12},
		{10, 14, 11, 15, 15, 12, 13, 12, 13, 11, 13, 11, 11, 10, 11, 11},
		{10, 11, 13, 14, 14, 11, 13, 11, 12, 12, 11, 11, 11, 11, 10, 12},
		{9, 11, 11, 12, 12, 12, 12, 11, 13, 13, 13, 11, 9, 9, 9, 9},
		{10, 13, 11, 14, 14, 12, 15, 12, 12, 13, 11, 12, 12, 11, 11, 11},
		{8, 14, 9, 9, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8, 8},
		{8, 14, 14, 11, 13, 10, 13, 13, 11, 12, 12, 15, 15, 12, 12, 12},
		{11, 11, 15, 11, 13, 12, 11, 11, 11, 10, 10, 11, 11, 11, 10, 11},
		{8, 8, 9, 8, 8, 8, 10, 9, 10, 9, 9, 10, 10, 10, 9, 9},
		{8, 11, 10, 13, 11, 11, 10, 11, 10, 9, 8, 8, 9, 8, 8, 9},
		{11, 13, 13, 12, 15, 13, 11, 11, 10, 11, 10, 10, 9, 8, 9, 8},
		{10, 11, 13, 11, 12, 11, 11, 11, 10, 9, 10, 14, 12, 8, 8, 8},
	}

	// A+B is Narrowband/Mediumband Prediction Weights
	// C+D is Wideband Prediction Weights
	//
	// +-------------+-----+-----+-----+-----+
	// | Coefficient |   A |   B |   C |   D |
	// +-------------+-----+-----+-----+-----+
	// | 0           | 179 | 116 | 175 |  68 |
	// |             |     |     |     |     |
	// | 1           | 138 |  67 | 148 |  62 |
	// |             |     |     |     |     |
	// | 2           | 140 |  82 | 160 |  66 |
	// |             |     |     |     |     |
	// | 3           | 148 |  59 | 176 |  60 |
	// |             |     |     |     |     |
	// | 4           | 151 |  92 | 178 |  72 |
	// |             |     |     |     |     |
	// | 5           | 149 |  72 | 173 | 117 |
	// |             |     |     |     |     |
	// | 6           | 153 | 100 | 174 |  85 |
	// |             |     |     |     |     |
	// | 7           | 151 |  89 | 164 |  90 |
	// |             |     |     |     |     |
	// | 8           | 163 |  92 | 177 | 118 |
	// |             |     |     |     |     |
	// | 9           |     |     | 174 | 136 |
	// |             |     |     |     |     |
	// | 10          |     |     | 196 | 151 |
	// |             |     |     |     |     |
	// | 11          |     |     | 182 | 142 |
	// |             |     |     |     |     |
	// | 12          |     |     | 198 | 160 |
	// |             |     |     |     |     |
	// | 13          |     |     | 192 | 142 |
	// |             |     |     |     |     |
	// | 14          |     |     | 182 | 155 |
	// +-------------+-----+-----+-----+-----+
	//
	// Table 20: Prediction Weights for Normalized LSF Decoding

	predictionWeightForNarrowbandAndMediumbandNormalizedLSF = [][]uint{
		{179, 138, 140, 148, 151, 149, 153, 151, 163},
		{116, 67, 82, 59, 92, 72, 100, 89, 92},
	}

	predictionWeightForWidebandNormalizedLSF = [][]uint{
		{175, 148, 160, 176, 178, 173, 174, 164, 177, 174, 196, 182, 198, 192, 182},
		{68, 62, 66, 60, 72, 117, 85, 90, 118, 136, 151, 142, 160, 142, 155},
	}

	//  +----+-------------------+
	//  | I1 | Coefficient       |
	//  +----+-------------------+
	//  |    | 0 1 2 3 4 5 6 7 8 |
	//  |    |                   |
	//  | 0  | A B A A A A A A A |
	//  |    |                   |
	//  | 1  | B A A A A A A A A |
	//  |    |                   |
	//  | 2  | A A A A A A A A A |
	//  |    |                   |
	//  | 3  | B B B A A A A B A |
	//  |    |                   |
	//  | 4  | A B A A A A A A A |
	//  |    |                   |
	//  | 5  | A B A A A A A A A |
	//  |    |                   |
	//  | 6  | B A B B A A A B A |
	//  |    |                   |
	//  | 7  | A B B A A B B A A |
	//  |    |                   |
	//  | 8  | A A B B A B A B B |
	//  |    |                   |
	//  | 9  | A A B B A A B B B |
	//  |    |                   |
	//  | 10 | A A A A A A A A A |
	//  |    |                   |
	//  | 11 | A B A B B B B B A |
	//  |    |                   |
	//  | 12 | A B A B B B B B A |
	//  |    |                   |
	//  | 13 | A B B B B B B B A |
	//  |    |                   |
	//  | 14 | B A B B A B B B B |
	//  |    |                   |
	//  | 15 | A B B B B B A B A |
	//  |    |                   |
	//  | 16 | A A B B A B A B A |
	//  |    |                   |
	//  | 17 | A A B B B A B B B |
	//  |    |                   |
	//  | 18 | A B B A A B B B A |
	//  |    |                   |
	//  | 19 | A A A B B B A B A |
	//  |    |                   |
	//  | 20 | A B B A A B A B A |
	//  |    |                   |
	//  | 21 | A B B A A A B B A |
	//  |    |                   |
	//  | 22 | A A A A A B B B B |
	//  |    |                   |
	//  | 23 | A A B B A A A B B |
	//  |    |                   |
	//  | 24 | A A A B A B B B B |
	//  |    |                   |
	//  | 25 | A B B B B B B B A |
	//  |    |                   |
	//  | 26 | A A A A A A A A A |
	//  |    |                   |
	//  | 27 | A A A A A A A A A |
	//  |    |                   |
	//  | 28 | A A B A B B A B A |
	//  |    |                   |
	//  | 29 | B A A B A A A A A |
	//  |    |                   |
	//  | 30 | A A A B B A B A B |
	//  |    |                   |
	//  | 31 | B A B B A B B B B |
	//  +----+-------------------+
	//  Table 21: Prediction Weight Selection for NB/MB Normalized LSF
	//            Decoding
	//
	// https://datatracker.ietf.org/doc/html/rfc6716#section-4.2.7.5.2
	predictionWeightSelectionForNarrowbandAndMediumbandNormalizedLSF = [][]uint{
		{0, 1, 0, 0, 0, 0, 0, 0, 0},
		{1, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{1, 1, 1, 0, 0, 0, 0, 1, 0},
		{0, 1, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 0, 0, 0, 0, 0, 0},
		{1, 0, 1, 1, 0, 0, 0, 1, 0},
		{0, 1, 1, 0, 0, 1, 1, 0, 0},
		{0, 0, 1, 1, 0, 1, 0, 1, 1},
		{0, 0, 1, 1, 0, 0, 1, 1, 1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 1, 1, 1, 1, 1, 0},
		{0, 1, 0, 1, 1, 1, 1, 1, 0},
		{0, 1, 1, 1, 1, 1, 1, 1, 0},
		{1, 0, 1, 1, 0, 1, 1, 1, 1},
		{0, 1, 1, 1, 1, 1, 0, 1, 0},
		{0, 0, 1, 1, 0, 1, 0, 1, 0},
		{0, 0, 1, 1, 1, 0, 1, 1, 1},
		{0, 1, 1, 0, 0, 1, 1, 1, 0},
		{0, 0, 0, 1, 1, 1, 0, 1, 0},
		{0, 1, 1, 0, 0, 1, 0, 1, 0},
		{0, 1, 1, 0, 0, 0, 1, 1, 0},
		{0, 0, 0, 0, 0, 1, 1, 1, 1},
		{0, 0, 1, 1, 0, 0, 0, 1, 1},
		{0, 0, 0, 1, 0, 1, 1, 1, 1},
		{0, 1, 1, 1, 1, 1, 1, 1, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 1, 0, 1, 1, 0, 1, 0},
		{1, 0, 0, 1, 0, 0, 0, 0, 0},
		{0, 0, 0, 1, 1, 0, 1, 0, 1},
		{1, 0, 1, 1, 0, 1, 1, 1, 1},
	}

	// +----+---------------------------------------------+
	// | I1 | Coefficient                                 |
	// +----+---------------------------------------------+
	// |    | 0  1  2  3  4  5  6  7  8  9 10 11 12 13 14 |
	// |    |                                             |
	// | 0  | C  C  C  C  C  C  C  C  C  C  C  C  C  C  D |
	// |    |                                             |
	// | 1  | C  C  C  C  C  C  C  C  C  C  C  C  C  C  C |
	// |    |                                             |
	// | 2  | C  C  D  C  C  D  D  D  C  D  D  D  D  C  C |
	// |    |                                             |
	// | 3  | C  C  C  C  C  C  C  C  C  C  C  C  D  C  C |
	// |    |                                             |
	// | 4  | C  D  D  C  D  C  D  D  C  D  D  D  D  D  C |
	// |    |                                             |
	// | 5  | C  C  D  C  C  C  C  C  C  C  C  C  C  C  C |
	// |    |                                             |
	// | 6  | D  C  C  C  C  C  C  C  C  C  C  D  C  D  C |
	// |    |                                             |
	// | 7  | C  D  D  C  C  C  D  C  D  D  D  C  D  C  D |
	// |    |                                             |
	// | 8  | C  D  C  D  D  C  D  C  D  C  D  D  D  D  D |
	// |    |                                             |
	// | 9  | C  C  C  C  C  C  C  C  C  C  C  C  C  C  D |
	// |    |                                             |
	// | 10 | C  D  C  C  C  C  C  C  C  C  C  C  C  C  C |
	// |    |                                             |
	// | 11 | C  C  D  C  D  D  D  D  D  D  D  C  D  C  C |
	// |    |                                             |
	// | 12 | C  C  D  C  C  D  C  D  C  D  C  C  D  C  C |
	// |    |                                             |
	// | 13 | C  C  C  C  D  D  C  D  C  D  D  D  D  C  C |
	// |    |                                             |
	// | 14 | C  D  C  C  C  D  D  C  D  D  D  C  D  D  D |
	// |    |                                             |
	// | 15 | C  C  D  D  C  C  C  C  C  C  C  C  D  D  C |
	// |    |                                             |
	// | 16 | C  D  D  C  D  C  D  D  D  D  D  C  D  C  C |
	// |    |                                             |
	// | 17 | C  C  D  C  C  C  C  D  C  C  D  D  D  C  C |
	// |    |                                             |
	// | 18 | C  C  C  C  C  C  C  C  C  C  C  C  C  C  D |
	// |    |                                             |
	// | 19 | C  C  C  C  C  C  C  C  C  C  C  C  D  C  C |
	// |    |                                             |
	// | 20 | C  C  C  C  C  C  C  C  C  C  C  C  C  C  C |
	// |    |                                             |
	// | 21 | C  D  C  D  C  D  D  C  D  C  D  C  D  D  C |
	// |    |                                             |
	// | 22 | C  C  D  D  D  D  C  D  D  C  C  D  D  C  C |
	// |    |                                             |
	// | 23 | C  D  D  C  D  C  D  C  D  C  C  C  C  D  C |
	// |    |                                             |
	// | 24 | C  C  C  D  D  C  D  C  D  D  D  D  D  D  D |
	// |    |                                             |
	// | 25 | C  C  C  C  C  C  C  C  C  C  C  C  C  C  D |
	// |    |                                             |
	// | 26 | C  D  D  C  C  C  D  D  C  C  D  D  D  D  D |
	// |    |                                             |
	// | 27 | C  C  C  C  C  D  C  D  D  D  D  C  D  D  D |
	// |    |                                             |
	// | 28 | C  C  C  C  C  C  C  C  C  C  C  C  C  C  D |
	// |    |                                             |
	// | 29 | C  C  C  C  C  C  C  C  C  C  C  C  C  C  D |
	// |    |                                             |
	// | 30 | D  C  C  C  C  C  C  C  C  C  C  D  C  C  C |
	// |    |                                             |
	// | 31 | C  C  D  C  C  D  D  D  C  C  D  C  C  D  C |
	// +----+---------------------------------------------+
	//
	// Table 22: Prediction Weight Selection for WB Normalized LSF Decoding
	predictionWeightSelectionForWidebandNormalizedLSF = [][]uint{
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 1, 0, 0, 1, 1, 1, 0, 1, 1, 1, 1, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0},
		{0, 1, 1, 0, 1, 0, 1, 1, 0, 1, 1, 1, 1, 1, 0},
		{0, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 1, 0},
		{0, 1, 1, 0, 0, 0, 1, 0, 1, 1, 1, 0, 1, 0, 1},
		{0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 1, 1, 1, 1, 1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{0, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1, 0, 1, 0, 0},
		{0, 0, 1, 0, 0, 1, 0, 1, 0, 1, 0, 0, 1, 0, 0},
		{0, 0, 0, 0, 1, 1, 0, 1, 0, 1, 1, 1, 1, 0, 0},
		{0, 1, 0, 0, 0, 1, 1, 0, 1, 1, 1, 0, 1, 1, 1},
		{0, 0, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 1, 1, 0},
		{0, 1, 1, 0, 1, 0, 1, 1, 1, 1, 1, 0, 1, 0, 0},
		{0, 0, 1, 0, 0, 0, 0, 1, 0, 0, 1, 1, 1, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
		{0, 1, 0, 1, 0, 1, 1, 0, 1, 0, 1, 0, 1, 1, 0},
		{0, 0, 1, 1, 1, 1, 0, 1, 1, 0, 0, 1, 1, 0, 0},
		{0, 1, 1, 0, 1, 0, 1, 0, 1, 0, 0, 0, 0, 1, 0},
		{0, 0, 0, 1, 1, 0, 1, 0, 1, 1, 1, 1, 1, 1, 1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{0, 1, 1, 0, 0, 0, 1, 1, 0, 0, 1, 1, 1, 1, 1},
		{0, 0, 0, 0, 0, 1, 0, 1, 1, 1, 1, 0, 1, 1, 1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1},
		{1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 1, 0, 0, 0},
		{0, 0, 1, 0, 0, 1, 1, 1, 0, 0, 1, 0, 0, 1, 0},
	}

	// +----+----------------------------------------+
	// | I1 | Codebook (Q8)                          |
	// +----+----------------------------------------+
	// |    |  0   1   2   3   4   5   6   7   8   9 |
	// |    |                                        |
	// | 0  | 12  35  60  83 108 132 157 180 206 228 |
	// |    |                                        |
	// | 1  | 15  32  55  77 101 125 151 175 201 225 |
	// |    |                                        |
	// | 2  | 19  42  66  89 114 137 162 184 209 230 |
	// |    |                                        |
	// | 3  | 12  25  50  72  97 120 147 172 200 223 |
	// |    |                                        |
	// | 4  | 26  44  69  90 114 135 159 180 205 225 |
	// |    |                                        |
	// | 5  | 13  22  53  80 106 130 156 180 205 228 |
	// |    |                                        |
	// | 6  | 15  25  44  64  90 115 142 168 196 222 |
	// |    |                                        |
	// | 7  | 19  24  62  82 100 120 145 168 190 214 |
	// |    |                                        |
	// | 8  | 22  31  50  79 103 120 151 170 203 227 |
	// |    |                                        |
	// | 9  | 21  29  45  65 106 124 150 171 196 224 |
	// |    |                                        |
	// | 10 | 30  49  75  97 121 142 165 186 209 229 |
	// |    |                                        |
	// | 11 | 19  25  52  70  93 116 143 166 192 219 |
	// |    |                                        |
	// | 12 | 26  34  62  75  97 118 145 167 194 217 |
	// |    |                                        |
	// | 13 | 25  33  56  70  91 113 143 165 196 223 |
	// |    |                                        |
	// | 14 | 21  34  51  72  97 117 145 171 196 222 |
	// |    |                                        |
	// | 15 | 20  29  50  67  90 117 144 168 197 221 |
	// |    |                                        |
	// | 16 | 22  31  48  66  95 117 146 168 196 222 |
	// |    |                                        |
	// | 17 | 24  33  51  77 116 134 158 180 200 224 |
	// |    |                                        |
	// | 18 | 21  28  70  87 106 124 149 170 194 217 |
	// |    |                                        |
	// | 19 | 26  33  53  64  83 117 152 173 204 225 |
	// |    |                                        |
	// | 20 | 27  34  65  95 108 129 155 174 210 225 |
	// |    |                                        |
	// | 21 | 20  26  72  99 113 131 154 176 200 219 |
	// |    |                                        |
	// | 22 | 34  43  61  78  93 114 155 177 205 229 |
	// |    |                                        |
	// | 23 | 23  29  54  97 124 138 163 179 209 229 |
	// |    |                                        |
	// | 24 | 30  38  56  89 118 129 158 178 200 231 |
	// |    |                                        |
	// | 25 | 21  29  49  63  85 111 142 163 193 222 |
	// |    |                                        |
	// | 26 | 27  48  77 103 133 158 179 196 215 232 |
	// |    |                                        |
	// | 27 | 29  47  74  99 124 151 176 198 220 237 |
	// |    |                                        |
	// | 28 | 33  42  61  76  93 121 155 174 207 225 |
	// |    |                                        |
	// | 29 | 29  53  87 112 136 154 170 188 208 227 |
	// |    |                                        |
	// | 30 | 24  30  52  84 131 150 166 186 203 229 |
	// |    |                                        |
	// | 31 | 37  48  64  84 104 118 156 177 201 230 |
	// +----+----------------------------------------+
	//
	// Table 23: NB/MB Normalized LSF Stage-1 Codebook Vectors
	codebookNormalizedLSFStageOneNarrowbandOrMediumband = [][]uint{
		{12, 35, 60, 83, 108, 132, 157, 180, 206, 228},
		{15, 32, 55, 77, 101, 125, 151, 175, 201, 225},
		{19, 42, 66, 89, 114, 137, 162, 184, 209, 230},
		{12, 25, 50, 72, 97, 120, 147, 172, 200, 223},
		{26, 44, 69, 90, 114, 135, 159, 180, 205, 225},
		{13, 22, 53, 80, 106, 130, 156, 180, 205, 228},
		{15, 25, 44, 64, 90, 115, 142, 168, 196, 222},
		{19, 24, 62, 82, 100, 120, 145, 168, 190, 214},
		{22, 31, 50, 79, 103, 120, 151, 170, 203, 227},
		{21, 29, 45, 65, 106, 124, 150, 171, 196, 224},
		{30, 49, 75, 97, 121, 142, 165, 186, 209, 229},
		{19, 25, 52, 70, 93, 116, 143, 166, 192, 219},
		{26, 34, 62, 75, 97, 118, 145, 167, 194, 217},
		{25, 33, 56, 70, 91, 113, 143, 165, 196, 223},
		{21, 34, 51, 72, 97, 117, 145, 171, 196, 222},
		{20, 29, 50, 67, 90, 117, 144, 168, 197, 221},
		{22, 31, 48, 66, 95, 117, 146, 168, 196, 222},
		{24, 33, 51, 77, 116, 134, 158, 180, 200, 224},
		{21, 28, 70, 87, 106, 124, 149, 170, 194, 217},
		{26, 33, 53, 64, 83, 117, 152, 173, 204, 225},
		{27, 34, 65, 95, 108, 129, 155, 174, 210, 225},
		{20, 26, 72, 99, 113, 131, 154, 176, 200, 219},
		{34, 43, 61, 78, 93, 114, 155, 177, 205, 229},
		{23, 29, 54, 97, 124, 138, 163, 179, 209, 229},
		{30, 38, 56, 89, 118, 129, 158, 178, 200, 231},
		{21, 29, 49, 63, 85, 111, 142, 163, 193, 222},
		{27, 48, 77, 103, 133, 158, 179, 196, 215, 232},
		{29, 47, 74, 99, 124, 151, 176, 198, 220, 237},
		{33, 42, 61, 76, 93, 121, 155, 174, 207, 225},
		{29, 53, 87, 112, 136, 154, 170, 188, 208, 227},
		{24, 30, 52, 84, 131, 150, 166, 186, 203, 229},
		{37, 48, 64, 84, 104, 118, 156, 177, 201, 230},
	}

	// +----+------------------------------------------------------------+
	// | I1 | Codebook (Q8)                                              |
	// +----+------------------------------------------------------------+
	// |    |  0  1  2  3  4   5   6   7   8   9  10  11  12  13  14  15 |
	// |    |                                                            |
	// | 0  |  7 23 38 54 69  85 100 116 131 147 162 178 193 208 223 239 |
	// |    |                                                            |
	// | 1  | 13 25 41 55 69  83  98 112 127 142 157 171 187 203 220 236 |
	// |    |                                                            |
	// | 2  | 15 21 34 51 61  78  92 106 126 136 152 167 185 205 225 240 |
	// |    |                                                            |
	// | 3  | 10 21 36 50 63  79  95 110 126 141 157 173 189 205 221 237 |
	// |    |                                                            |
	// | 4  | 17 20 37 51 59  78  89 107 123 134 150 164 184 205 224 240 |
	// |    |                                                            |
	// | 5  | 10 15 32 51 67  81  96 112 129 142 158 173 189 204 220 236 |
	// |    |                                                            |
	// | 6  |  8 21 37 51 65  79  98 113 126 138 155 168 179 192 209 218 |
	// |    |                                                            |
	// | 7  | 12 15 34 55 63  78  87 108 118 131 148 167 185 203 219 236 |
	// |    |                                                            |
	// | 8  | 16 19 32 36 56  79  91 108 118 136 154 171 186 204 220 237 |
	// |    |                                                            |
	// | 9  | 11 28 43 58 74  89 105 120 135 150 165 180 196 211 226 241 |
	// |    |                                                            |
	// | 10 |  6 16 33 46 60  75  92 107 123 137 156 169 185 199 214 225 |
	// |    |                                                            |
	// | 11 | 11 19 30 44 57  74  89 105 121 135 152 169 186 202 218 234 |
	// |    |                                                            |
	// | 12 | 12 19 29 46 57  71  88 100 120 132 148 165 182 199 216 233 |
	// |    |                                                            |
	// | 13 | 17 23 35 46 56  77  92 106 123 134 152 167 185 204 222 237 |
	// |    |                                                            |
	// | 14 | 14 17 45 53 63  75  89 107 115 132 151 171 188 206 221 240 |
	// |    |                                                            |
	// | 15 |  9 16 29 40 56  71  88 103 119 137 154 171 189 205 222 237 |
	// |    |                                                            |
	// | 16 | 16 19 36 48 57  76  87 105 118 132 150 167 185 202 218 236 |
	// |    |                                                            |
	// | 17 | 12 17 29 54 71  81  94 104 126 136 149 164 182 201 221 237 |
	// |    |                                                            |
	// | 18 | 15 28 47 62 79  97 115 129 142 155 168 180 194 208 223 238 |
	// |    |                                                            |
	// | 19 |  8 14 30 45 62  78  94 111 127 143 159 175 192 207 223 239 |
	// |    |                                                            |
	// | 20 | 17 30 49 62 79  92 107 119 132 145 160 174 190 204 220 235 |
	// |    |                                                            |
	// | 21 | 14 19 36 45 61  76  91 108 121 138 154 172 189 205 222 238 |
	// |    |                                                            |
	// | 22 | 12 18 31 45 60  76  91 107 123 138 154 171 187 204 221 236 |
	// |    |                                                            |
	// | 23 | 13 17 31 43 53  70  83 103 114 131 149 167 185 203 220 237 |
	// |    |                                                            |
	// | 24 | 17 22 35 42 58  78  93 110 125 139 155 170 188 206 224 240 |
	// |    |                                                            |
	// | 25 |  8 15 34 50 67  83  99 115 131 146 162 178 193 209 224 239 |
	// |    |                                                            |
	// | 26 | 13 16 41 66 73  86  95 111 128 137 150 163 183 206 225 241 |
	// |    |                                                            |
	// | 27 | 17 25 37 52 63  75  92 102 119 132 144 160 175 191 212 231 |
	// |    |                                                            |
	// | 28 | 19 31 49 65 83 100 117 133 147 161 174 187 200 213 227 242 |
	// |    |                                                            |
	// | 29 | 18 31 52 68 88 103 117 126 138 149 163 177 192 207 223 239 |
	// |    |                                                            |
	// | 30 | 16 29 47 61 76  90 106 119 133 147 161 176 193 209 224 240 |
	// |    |                                                            |
	// | 31 | 15 21 35 50 61  73  86  97 110 119 129 141 175 198 218 237 |
	// +----+------------------------------------------------------------+
	// Table 24: WB Normalized LSF Stage-1 Codebook Vectors
	codebookNormalizedLSFStageOneWideband = [][]uint{
		{7, 23, 38, 54, 69, 85, 100, 116, 131, 147, 162, 178, 193, 208, 223, 239},
		{13, 25, 41, 55, 69, 83, 98, 112, 127, 142, 157, 171, 187, 203, 220, 236},
		{15, 21, 34, 51, 61, 78, 92, 106, 126, 136, 152, 167, 185, 205, 225, 240},
		{10, 21, 36, 50, 63, 79, 95, 110, 126, 141, 157, 173, 189, 205, 221, 237},
		{17, 20, 37, 51, 59, 78, 89, 107, 123, 134, 150, 164, 184, 205, 224, 240},
		{10, 15, 32, 51, 67, 81, 96, 112, 129, 142, 158, 173, 189, 204, 220, 236},
		{8, 21, 37, 51, 65, 79, 98, 113, 126, 138, 155, 168, 179, 192, 209, 218},
		{12, 15, 34, 55, 63, 78, 87, 108, 118, 131, 148, 167, 185, 203, 219, 236},
		{16, 19, 32, 36, 56, 79, 91, 108, 118, 136, 154, 171, 186, 204, 220, 237},
		{11, 28, 43, 58, 74, 89, 105, 120, 135, 150, 165, 180, 196, 211, 226, 241},
		{6, 16, 33, 46, 60, 75, 92, 107, 123, 137, 156, 169, 185, 199, 214, 225},
		{11, 19, 30, 44, 57, 74, 89, 105, 121, 135, 152, 169, 186, 202, 218, 234},
		{12, 19, 29, 46, 57, 71, 88, 100, 120, 132, 148, 165, 182, 199, 216, 233},
		{17, 23, 35, 46, 56, 77, 92, 106, 123, 134, 152, 167, 185, 204, 222, 237},
		{14, 17, 45, 53, 63, 75, 89, 107, 115, 132, 151, 171, 188, 206, 221, 240},
		{9, 16, 29, 40, 56, 71, 88, 103, 119, 137, 154, 171, 189, 205, 222, 237},
		{16, 19, 36, 48, 57, 76, 87, 105, 118, 132, 150, 167, 185, 202, 218, 236},
		{12, 17, 29, 54, 71, 81, 94, 104, 126, 136, 149, 164, 182, 201, 221, 237},
		{15, 28, 47, 62, 79, 97, 115, 129, 142, 155, 168, 180, 194, 208, 223, 238},
		{8, 14, 30, 45, 62, 78, 94, 111, 127, 143, 159, 175, 192, 207, 223, 239},
		{17, 30, 49, 62, 79, 92, 107, 119, 132, 145, 160, 174, 190, 204, 220, 235},
		{14, 19, 36, 45, 61, 76, 91, 108, 121, 138, 154, 172, 189, 205, 222, 238},
		{12, 18, 31, 45, 60, 76, 91, 107, 123, 138, 154, 171, 187, 204, 221, 236},
		{13, 17, 31, 43, 53, 70, 83, 103, 114, 131, 149, 167, 185, 203, 220, 237},
		{17, 22, 35, 42, 58, 78, 93, 110, 125, 139, 155, 170, 188, 206, 224, 240},
		{8, 15, 34, 50, 67, 83, 99, 115, 131, 146, 162, 178, 193, 209, 224, 239},
		{13, 16, 41, 66, 73, 86, 95, 111, 128, 137, 150, 163, 183, 206, 225, 241},
		{17, 25, 37, 52, 63, 75, 92, 102, 119, 132, 144, 160, 175, 191, 212, 231},
		{19, 31, 49, 65, 83, 100, 117, 133, 147, 161, 174, 187, 200, 213, 227, 242},
		{18, 31, 52, 68, 88, 103, 117, 126, 138, 149, 163, 177, 192, 207, 223, 239},
		{16, 29, 47, 61, 76, 90, 106, 119, 133, 147, 161, 176, 193, 209, 224, 240},
		{15, 21, 35, 50, 61, 73, 86, 97, 110, 119, 129, 141, 175, 198, 218, 237},
	}

	//  +-------------+-----------+----+
	//  | Coefficient | NB and MB | WB |
	//  +-------------+-----------+----+
	//  | 0           |         0 |  0 |
	//  |             |           |    |
	//  | 1           |         9 | 15 |
	//  |             |           |    |
	//  | 2           |         6 |  8 |
	//  |             |           |    |
	//  | 3           |         3 |  7 |
	//  |             |           |    |
	//  | 4           |         4 |  4 |
	//  |             |           |    |
	//  | 5           |         5 | 11 |
	//  |             |           |    |
	//  | 6           |         8 | 12 |
	//  |             |           |    |
	//  | 7           |         1 |  3 |
	//  |             |           |    |
	//  | 8           |         2 |  2 |
	//  |             |           |    |
	//  | 9           |         7 | 13 |
	//  |             |           |    |
	//  | 10          |           | 10 |
	//  |             |           |    |
	//  | 11          |           |  5 |
	//  |             |           |    |
	//  | 12          |           |  6 |
	//  |             |           |    |
	//  | 13          |           |  9 |
	//  |             |           |    |
	//  | 14          |           | 14 |
	//  |             |           |    |
	//  | 15          |           |  1 |
	//  +-------------+-----------+----+
	//  Table 27: LSF Ordering for Polynomial Evaluation

	lsfOrderingForPolynomialEvaluationNarrowbandAndMediumband = []uint8{0, 9, 6, 3, 4, 5, 8, 1, 2, 7}
	lsfOrderingForPolynomialEvaluationWideband                = []uint8{0, 15, 8, 7, 4, 11, 12, 3, 2, 13, 10, 5, 6, 9, 14, 1}

	// +-----+-------+-------+-------+-------+
	// |   i |    +0 |    +1 |    +2 |    +3 |
	// +-----+-------+-------+-------+-------+
	// |   0 |  4096 |  4095 |  4091 |  4085 |
	// |     |       |       |       |       |
	// |   4 |  4076 |  4065 |  4052 |  4036 |
	// |     |       |       |       |       |
	// |   8 |  4017 |  3997 |  3973 |  3948 |
	// |     |       |       |       |       |
	// |  12 |  3920 |  3889 |  3857 |  3822 |
	// |     |       |       |       |       |
	// |  16 |  3784 |  3745 |  3703 |  3659 |
	// |     |       |       |       |       |
	// |  20 |  3613 |  3564 |  3513 |  3461 |
	// |     |       |       |       |       |
	// |  24 |  3406 |  3349 |  3290 |  3229 |
	// |     |       |       |       |       |
	// |  28 |  3166 |  3102 |  3035 |  2967 |
	// |     |       |       |       |       |
	// |  32 |  2896 |  2824 |  2751 |  2676 |
	// |     |       |       |       |       |
	// |  36 |  2599 |  2520 |  2440 |  2359 |
	// |     |       |       |       |       |
	// |  40 |  2276 |  2191 |  2106 |  2019 |
	// |     |       |       |       |       |
	// |  44 |  1931 |  1842 |  1751 |  1660 |
	// |     |       |       |       |       |
	// |  48 |  1568 |  1474 |  1380 |  1285 |
	// |     |       |       |       |       |
	// |  52 |  1189 |  1093 |   995 |   897 |
	// |     |       |       |       |       |
	// |  56 |   799 |   700 |   601 |   501 |
	// |     |       |       |       |       |
	// |  60 |   401 |   301 |   201 |   101 |
	// |     |       |       |       |       |
	// |  64 |     0 |  -101 |  -201 |  -301 |
	// |     |       |       |       |       |
	// |  68 |  -401 |  -501 |  -601 |  -700 |
	// |     |       |       |       |       |
	// |  72 |  -799 |  -897 |  -995 | -1093 |
	// |     |       |       |       |       |
	// |  76 | -1189 | -1285 | -1380 | -1474 |
	// |     |       |       |       |       |
	// |  80 | -1568 | -1660 | -1751 | -1842 |
	// |     |       |       |       |       |
	// |  84 | -1931 | -2019 | -2106 | -2191 |
	// |     |       |       |       |       |
	// |  88 | -2276 | -2359 | -2440 | -2520 |
	// |     |       |       |       |       |
	// |  92 | -2599 | -2676 | -2751 | -2824 |
	// |     |       |       |       |       |
	// |  96 | -2896 | -2967 | -3035 | -3102 |
	// |     |       |       |       |       |
	// | 100 | -3166 | -3229 | -3290 | -3349 |
	// |     |       |       |       |       |
	// | 104 | -3406 | -3461 | -3513 | -3564 |
	// |     |       |       |       |       |
	// | 108 | -3613 | -3659 | -3703 | -3745 |
	// |     |       |       |       |       |
	// | 112 | -3784 | -3822 | -3857 | -3889 |
	// |     |       |       |       |       |
	// | 116 | -3920 | -3948 | -3973 | -3997 |
	// |     |       |       |       |       |
	// | 120 | -4017 | -4036 | -4052 | -4065 |
	// |     |       |       |       |       |
	// | 124 | -4076 | -4085 | -4091 | -4095 |
	// |     |       |       |       |       |
	// | 128 | -4096 |       |       |       |
	// +-----+-------+-------+-------+-------+
	//
	// Table 28: Q12 Cosine Table for LSF Conversion
	q12CosineTableForLSFConverion = []int32{
		4096, 4095, 4091, 4085, 4076, 4065, 4052, 4036, 4017, 3997,
		3973, 3948, 3920, 3889, 3857, 3822, 3784, 3745, 3703, 3659,
		3613, 3564, 3513, 3461, 3406, 3349, 3290, 3229, 3166, 3102,
		3035, 2967, 2896, 2824, 2751, 2676, 2599, 2520, 2440, 2359,
		2276, 2191, 2106, 2019, 1931, 1842, 1751, 1660, 1568, 1474,
		1380, 1285, 1189, 1093, 995, 897, 799, 700, 601, 501,
		401, 301, 201, 101, 0, -101, -201, -301, -401, -501,
		-601, -700, -799, -897, -995, -1093, -1189, -1285, -1380, -1474,
		-1568, -1660, -1751, -1842, -1931, -2019, -2106, -2191, -2276, -2359,
		-2440, -2520, -2599, -2676, -2751, -2824, -2896, -2967, -3035, -3102,
		-3166, -3229, -3290, -3349, -3406, -3461, -3513, -3564, -3613, -3659,
		-3703, -3745, -3784, -3822, -3857, -3889, -3920, -3948, -3973, -3997,
		-4017, -4036, -4052, -4065, -4076, -4085, -4091, -4095, -4096,
	}
)
